package db

// oauth_test.go -- issue #37 phase 1 (ADR 0047 §14): the OAuth tables, CHECKs, one-way triggers,
// resolvers, the registry's op_* and the K-22 writer pin, against real Postgres. Every test here runs
// in opTx (one rolled-back owner transaction; identities switched with SET LOCAL SESSION
// AUTHORIZATION) and commits nothing. The concurrency proofs, which need commits, are in
// oauth_race_test.go.

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// oauthTenantTables are ADR 0047 §14.2's three tenant tables.
var oauthTenantTables = []string{"oauth_grants", "oauth_codes", "oauth_tokens"}

// oauthScopeSet is ADR 0047 §8.2's closed scope set, in the CHECKs' order. webhooks:manage is NOT in
// it (§13).
var oauthScopeSet = []string{"timesheets:read", "employees:read", "employees:write", "locations:write", "punches:read"}

// oauthRedirectRule is the ONE redirect expression (00104 §1) as it must appear, byte for byte, in
// oauth_clients_redirect_uris_shape and in the bodies of op_register_oauth_client and
// op_set_oauth_client_redirects.
const oauthRedirectRule = `^(https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?(:443)?|http://(localhost|127\.0\.0\.1):[1-9][0-9]{0,4})/[A-Za-z0-9._~!$&()*+,;=:%/-]*$`

const oauthGoodRedirect = "https://maltacalculator.com/api/integrations/taptime/callback"

// oauthWorld is a business with an owner, an app the owner inserted into the registry and an active
// operator, all inside the test's transaction.
type oauthWorld struct {
	apiKeyWorld
	operator opAccount
	client   uuid.UUID
}

func newOAuthWorld(t *testing.T, ctx context.Context, tx pgx.Tx) oauthWorld {
	t.Helper()
	w := oauthWorld{apiKeyWorld: newAPIKeyWorld(t, ctx, tx), operator: opNewActive(t, ctx, tx)}
	w.client = oauthInsertClient(t, ctx, tx, w.operator.id)
	return w
}

// oauthInsertClient writes an app as the owner -- the registry's rows are op_*'s in the product;
// this is the fixture's shortcut, and it passes every CHECK.
func oauthInsertClient(t *testing.T, ctx context.Context, tx pgx.Tx, operator uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO public.oauth_clients (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by)
	                            VALUES ('Malta Calculator', $1, $2, $3, 'tt_cs_live_AbCd', $4) RETURNING id`,
		[]string{oauthGoodRedirect}, []string{"timesheets:read", "employees:read", "employees:write"}, randHash(t), operator).Scan(&id); err != nil {
		t.Fatalf("insert a client: %v", err)
	}
	return id
}

// oauthAs runs fn as tappa_app inside tenant's context (a savepoint of tx) with the generated store.
func oauthAs(t *testing.T, ctx context.Context, tx pgx.Tx, tenant uuid.UUID, fn func(q *store.Queries, sp pgx.Tx) error) error {
	t.Helper()
	return idemAs(t, ctx, tx, tenant, func(sp pgx.Tx) error { return fn(store.New(sp), sp) })
}

// grant writes a live grant, a refresh token and an access token for w's business through the
// shipped statements, and returns their ids.
func (w oauthWorld) grant(t *testing.T, ctx context.Context, tx pgx.Tx, scopes []string) (grant, refresh, access uuid.UUID) {
	t.Helper()
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		g, err := q.InsertOAuthGrant(ctx, store.InsertOAuthGrantParams{TenantID: w.tenant, ClientID: w.client, Scopes: scopes, CreatedBy: w.owner})
		if err != nil {
			return err
		}
		grant = g.ID
		r, err := q.InsertOAuthToken(ctx, store.InsertOAuthTokenParams{TenantID: w.tenant, GrantID: grant, Kind: "refresh", TokenHash: randHash(t)})
		if err != nil {
			return err
		}
		refresh = r.ID
		a, err := q.InsertOAuthToken(ctx, store.InsertOAuthTokenParams{TenantID: w.tenant, GrantID: grant, Kind: "access", TokenHash: randHash(t), Scopes: scopes})
		if err != nil {
			return err
		}
		access = a.ID
		return nil
	}); err != nil {
		t.Fatalf("write a grant and its pair: %v", err)
	}
	return grant, refresh, access
}

// ---------------------------------------------------------------- the schema --

// TestOAuthCatalog_TheTenantTablesCarryTheFive -- CLAUDE.md §6 on the three tenant tables, in the
// live catalogue: tenant_id NOT NULL, ENABLE and FORCE, the standard NULLIF policy with USING and
// WITH CHECK (and, on codes and tokens, a RESTRICTIVE delete policy and nothing else), an index led by
// tenant_id; tappa_app reads no hash column, holds no TRUNCATE, REFERENCES or TRIGGER, never DELETEs a
// grant, and cannot write created_at.
func TestOAuthCatalog_TheTenantTablesCarryTheFive(t *testing.T) {
	ctx, tx := opTx(t)
	const isolation = "(tenant_id = (NULLIF(current_setting('app.tenant_id'::text, true), ''::text))::uuid)"
	for _, table := range oauthTenantTables {
		var enabled, forced, notNull bool
		if err := tx.QueryRow(ctx, `
			SELECT c.relrowsecurity, c.relforcerowsecurity, a.attnotnull
			  FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id'
			 WHERE c.oid = ('public.' || $1)::regclass`, table).Scan(&enabled, &forced, &notNull); err != nil {
			t.Fatalf("%s: %v", table, err)
		}
		if !enabled || !forced || !notNull {
			t.Errorf("%s: ENABLE %v FORCE %v tenant_id NOT NULL %v; all three are required", table, enabled, forced, notNull)
		}
		rows, err := tx.Query(ctx, `SELECT polname, polpermissive, polcmd::text, coalesce(pg_get_expr(polqual, polrelid), ''),
		                                   coalesce(pg_get_expr(polwithcheck, polrelid), '')
		                              FROM pg_policy WHERE polrelid = ('public.' || $1)::regclass ORDER BY polname`, table)
		if err != nil {
			t.Fatal(err)
		}
		var isolated, restrictive int
		for rows.Next() {
			var name, cmd, using, check string
			var permissive bool
			if err := rows.Scan(&name, &permissive, &cmd, &using, &check); err != nil {
				t.Fatal(err)
			}
			switch {
			case permissive && cmd == "*" && using == isolation && check == isolation:
				isolated++
			case !permissive && cmd == "d" && strings.HasPrefix(using, "("+isolation+" AND") && check == "":
				restrictive++
			default:
				t.Errorf("%s: policy %s (permissive %v, cmd %s) USING %q WITH CHECK %q is neither the isolation policy nor a restrictive delete bound", table, name, permissive, cmd, using, check)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		wantRestrictive := 1
		if table == "oauth_grants" {
			wantRestrictive = 0
		}
		if isolated != 1 || restrictive != wantRestrictive {
			t.Errorf("%s: %d isolation and %d restrictive delete policies, want 1 and %d", table, isolated, restrictive, wantRestrictive)
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
		                            WHERE i.indrelid = ('public.' || $1)::regclass AND a.attname = 'tenant_id' AND i.indpred IS NULL`, table); n < 1 {
			t.Errorf("%s: no full index leads with tenant_id", table)
		}
		for _, verb := range []string{"TRUNCATE", "REFERENCES", "TRIGGER"} {
			if n := opInt(t, ctx, tx, `SELECT count(*) WHERE has_table_privilege('tappa_app', ('public.' || $1)::regclass, $2)`, table, verb); n != 0 {
				t.Errorf("tappa_app holds %s on %s", verb, table)
			}
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_attribute a WHERE a.attrelid = ('public.' || $1)::regclass AND a.attname = 'created_at'
		                            AND has_column_privilege('tappa_app', a.attrelid, a.attnum, 'INSERT')`, table); n != 0 {
			t.Errorf("tappa_app may write %s.created_at -- the one clock is the DEFAULT", table)
		}
	}
	for _, c := range [][2]string{{"oauth_codes", "code_hash"}, {"oauth_tokens", "token_hash"}} {
		if n := opInt(t, ctx, tx, `SELECT count(*) WHERE has_column_privilege('tappa_app', ('public.' || $1)::regclass, $2, 'SELECT')`, c[0], c[1]); n != 0 {
			t.Errorf("tappa_app reads %s.%s -- the resolvers are its one reader", c[0], c[1])
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) WHERE has_table_privilege('tappa_app', 'public.oauth_grants', 'DELETE')`); n != 0 {
		t.Error("tappa_app may DELETE a grant; grants are revoked, never deleted")
	}
	// The global UNIQUEs that bound the resolvers (ADR 0002 md.7 (ii)) and O-8's partial UNIQUE.
	for _, idx := range []struct{ name, def string }{
		{"oauth_codes_code_hash_key", "CREATE UNIQUE INDEX oauth_codes_code_hash_key ON public.oauth_codes USING btree (code_hash)"},
		{"oauth_tokens_token_hash_key", "CREATE UNIQUE INDEX oauth_tokens_token_hash_key ON public.oauth_tokens USING btree (token_hash)"},
		{"oauth_grants_live_client_key", "CREATE UNIQUE INDEX oauth_grants_live_client_key ON public.oauth_grants USING btree (tenant_id, client_id) WHERE (revoked_at IS NULL)"},
	} {
		var def string
		if err := tx.QueryRow(ctx, `SELECT pg_get_indexdef(('public.' || $1)::regclass)`, idx.name).Scan(&def); err != nil || def != idx.def {
			t.Errorf("%s is %q (%v), want %q", idx.name, def, err, idx.def)
		}
	}
}

// TestOAuthClients_TheTenantScopelessTableRules -- CLAUDE.md §6's tenant-scope-less table: no column
// named tenant_id, ENABLE and FORCE together and NO policy, tappa_app reaching nothing (measured both
// in the catalogue and by a refused read), the R5 waiver visible in the Up, and the restore check's
// invariant (scripts/pg-restore-verify.sh refuses a restore whose ENABLE and FORCE counts differ) held
// over every public table.
func TestOAuthClients_TheTenantScopelessTableRules(t *testing.T) {
	ctx, tx := opTx(t)
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_attribute WHERE attrelid = 'public.oauth_clients'::regclass AND attname = 'tenant_id' AND NOT attisdropped`); n != 0 {
		t.Error("oauth_clients has a column named tenant_id; R5 and the RLS tests would read it as a tenant table")
	}
	var enabled, forced bool
	if err := tx.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = 'public.oauth_clients'::regclass`).Scan(&enabled, &forced); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced {
		t.Errorf("oauth_clients: ENABLE %v FORCE %v, want both", enabled, forced)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_policy WHERE polrelid = 'public.oauth_clients'::regclass`); n != 0 {
		t.Errorf("oauth_clients carries %d policies; a policy would be a door for somebody", n)
	}
	for _, verb := range []string{"SELECT", "INSERT", "UPDATE", "REFERENCES"} {
		if n := opInt(t, ctx, tx, `SELECT count(*) WHERE has_any_column_privilege('tappa_app', 'public.oauth_clients', $1)`, verb); n != 0 {
			t.Errorf("tappa_app holds %s on a column of oauth_clients", verb)
		}
	}
	for _, verb := range []string{"DELETE", "TRUNCATE", "TRIGGER"} {
		if n := opInt(t, ctx, tx, `SELECT count(*) WHERE has_table_privilege('tappa_app', 'public.oauth_clients', $1)`, verb); n != 0 {
			t.Errorf("tappa_app holds %s on oauth_clients", verb)
		}
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", `SELECT count(*) FROM public.oauth_clients`), sqlstateInsufficientPrivi, "tappa_app reading oauth_clients")
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class c WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r'
	                            AND c.relrowsecurity IS DISTINCT FROM c.relforcerowsecurity`); n != 0 {
		t.Errorf("%d public tables have ENABLE without FORCE or the reverse; pg-restore-verify.sh refuses such a restore", n)
	}
	up, _ := opMigrationSections(t, "00104_create_the_oauth_tables.sql")
	if !regexp.MustCompile(`(?m)^-- redline: no-tenant-scope\(oauth_clients\) .+`).MatchString(up) {
		t.Error("00104's Up carries no visible R5 waiver for oauth_clients")
	}
}

// TestOAuthRLS_TenantIsolation -- CLAUDE.md §8: on each tenant table, B's context neither reads,
// updates nor deletes A's row, and B cannot write a row stamped with A's tenant (WITH CHECK). The
// probes carry NO tenant_id filter (CLAUDE.md §6: with one, 0 rows would be the WHERE's doing and the
// test would stay green with RLS off); the positive control -- A's own context finds the row by the
// same probe -- keeps the zero from passing empty.
func TestOAuthRLS_TenantIsolation(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := newOAuthWorld(t, ctx, tx), newOAuthWorld(t, ctx, tx)
	grant, refresh, _ := a.grant(t, ctx, tx, []string{"timesheets:read"})
	var code uuid.UUID
	if err := oauthAs(t, ctx, tx, a.tenant, func(q *store.Queries, _ pgx.Tx) error {
		r, err := q.InsertOAuthCode(ctx, store.InsertOAuthCodeParams{TenantID: a.tenant, CodeHash: randHash(t), ClientID: a.client,
			CreatedBy: a.owner, RedirectUri: oauthGoodRedirect, Scopes: []string{"timesheets:read"}, CodeChallenge: strings.Repeat("A", 43)})
		code = r.ID
		return err
	}); err != nil {
		t.Fatalf("CONTROL: A writes a code in its own context: %v", err)
	}
	count := func(tenant uuid.UUID, sql string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := oauthAs(t, ctx, tx, tenant, func(_ *store.Queries, sp pgx.Tx) error { return sp.QueryRow(ctx, sql, args...).Scan(&n) }); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	for _, c := range []struct {
		table string
		id    uuid.UUID
		col   string
	}{{"oauth_grants", grant, "last_used_at"}, {"oauth_codes", code, "consumed_at"}, {"oauth_tokens", refresh, "used_at"}} {
		read := `SELECT count(*) FROM public.` + c.table + ` WHERE id = $1`
		if n := count(a.tenant, read, c.id); n != 1 {
			t.Fatalf("POSITIVE CONTROL: A's context sees %d of its %s rows, want 1", n, c.table)
		}
		if n := count(b.tenant, read, c.id); n != 0 {
			t.Errorf("B's context sees %d of A's %s rows, want 0", n, c.table)
		}
		if n := count(uuid.Nil, read, c.id); n != 0 {
			t.Errorf("the nil context sees %d of A's %s rows, want 0", n, c.table)
		}
		upd := `WITH u AS (UPDATE public.` + c.table + ` SET ` + c.col + ` = clock_timestamp() WHERE id = $1 RETURNING 1) SELECT count(*) FROM u`
		if n := count(b.tenant, upd, c.id); n != 0 {
			t.Errorf("B updated %d of A's %s rows", n, c.table)
		}
		if c.table != "oauth_grants" {
			del := `WITH d AS (DELETE FROM public.` + c.table + ` WHERE id = $1 RETURNING 1) SELECT count(*) FROM d`
			if n := count(b.tenant, del, c.id); n != 0 {
				t.Errorf("B deleted %d of A's %s rows", n, c.table)
			}
		}
	}
	// WITH CHECK: B stamping a row with A's tenant (and A's own grant, client and owner) is refused.
	// The probes carry NO RETURNING: a RETURNING insert is also held to the SELECT policy (USING), so
	// it would be refused with WITH CHECK gone (measured: the shipped RETURNING inserts stayed red
	// under `WITH CHECK (true)`); these are refused by WITH CHECK alone.
	for what, ins := range map[string]struct {
		sql  string
		args []any
	}{
		"a grant": {`INSERT INTO public.oauth_grants (tenant_id, client_id, scopes, created_by) VALUES ($1, $2, '{timesheets:read}', $3)`,
			[]any{a.tenant, a.client, a.owner}},
		"a token": {`INSERT INTO public.oauth_tokens (tenant_id, grant_id, kind, token_hash, expires_at) VALUES ($1, $2, 'refresh', $3, now() + interval '2160 hours')`,
			[]any{a.tenant, grant, randHash(t)}},
		"a code": {`INSERT INTO public.oauth_codes (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes, code_challenge) VALUES ($1, $2, $3, $4, $5, '{timesheets:read}', $6)`,
			[]any{a.tenant, randHash(t), a.client, a.owner, oauthGoodRedirect, strings.Repeat("A", 43)}},
	} {
		opWant(t, oauthAs(t, ctx, tx, b.tenant, func(_ *store.Queries, sp pgx.Tx) error {
			_, err := sp.Exec(ctx, ins.sql, ins.args...)
			return err
		}), sqlstateInsufficientPrivi, "B inserting "+what+" stamped with A's tenant")
		// POSITIVE CONTROL: the same statement in A's own context is accepted (the refusal is the
		// policy's, not the statement's).
		if err := oauthAs(t, ctx, tx, a.tenant, func(_ *store.Queries, sp pgx.Tx) error {
			if what == "a grant" {
				// One live grant per (business, app): the control revokes the fixture's first.
				if _, err := sp.Exec(ctx, `UPDATE public.oauth_grants SET revoked_at = clock_timestamp(), revoked_reason = 'owner' WHERE id = $1`, grant); err != nil {
					return err
				}
			}
			_, err := sp.Exec(ctx, ins.sql, ins.args...)
			return err
		}); err != nil {
			t.Errorf("CONTROL: A inserting %s in its own context: %v", what, err)
		}
	}
	// The composite key: B's own row cannot name A's grant.
	opWant(t, oauthAs(t, ctx, tx, b.tenant, func(q *store.Queries, _ pgx.Tx) error {
		_, err := q.InsertOAuthToken(ctx, store.InsertOAuthTokenParams{TenantID: b.tenant, GrantID: grant, Kind: "refresh", TokenHash: randHash(t)})
		return err
	}), sqlstateForeignKeyViolation, "B's token naming A's grant")
}

// TestOAuthChecks_LifetimesAreEqualitiesInEveryTimeZone -- ADR 0047 §14.2's one clock: a code lives
// exactly 60 seconds, an access token 1 hour, a refresh token 2160 hours -- a second less or more is
// refused, created_at and expires_at come from one now(); a refresh token born before a DST change
// takes a revoked_at written from a Europe/Malta session as from a UTC one (the CHECK re-runs on
// every UPDATE, and hours, unlike days, do not depend on the zone -- the contrast is measured).
func TestOAuthChecks_LifetimesAreEqualitiesInEveryTimeZone(t *testing.T) {
	ctx, tx := opTx(t)
	w := newOAuthWorld(t, ctx, tx)
	grant, refresh, access := w.grant(t, ctx, tx, []string{"timesheets:read"})
	// In seconds: a timestamptz difference prints as days, which says nothing about hours.
	var accessLife, refreshLife int64
	if err := tx.QueryRow(ctx, `SELECT (SELECT extract(epoch FROM expires_at - created_at)::bigint FROM oauth_tokens WHERE id = $1),
	                                   (SELECT extract(epoch FROM expires_at - created_at)::bigint FROM oauth_tokens WHERE id = $2)`, access, refresh).Scan(&accessLife, &refreshLife); err != nil {
		t.Fatal(err)
	}
	if accessLife != 3600 || refreshLife != 2160*3600 {
		t.Errorf("the shipped INSERT gave an access token %d s and a refresh token %d s, want 3600 and %d", accessLife, refreshLife, 2160*3600)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM oauth_tokens WHERE id IN ($1, $2) AND created_at = now()`, access, refresh); n != 2 {
		t.Errorf("%d of the two tokens were born at the transaction's now(), want 2", n)
	}
	try := func(sql string, args ...any) error {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = sp.Exec(ctx, sql, args...)
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			t.Fatal(rbErr)
		}
		return err
	}
	insToken := `INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, scopes, expires_at) VALUES ($1, $2, $3, $4, $5, now() + $6::interval)`
	for _, c := range []struct {
		kind, life string
		scopes     []string
		ok         bool
	}{
		{"access", "1 hour", []string{"timesheets:read"}, true},
		{"access", "59 minutes 59 seconds", []string{"timesheets:read"}, false},
		{"access", "1 hour 1 second", []string{"timesheets:read"}, false},
		{"refresh", "2160 hours", nil, true},
		{"refresh", "2159 hours 59 minutes 59 seconds", nil, false},
		{"refresh", "2160 hours 1 second", nil, false},
	} {
		err := try(insToken, w.tenant, grant, c.kind, randHash(t), c.scopes, c.life)
		if c.ok && err != nil {
			t.Errorf("a %s token living %s was refused: %v", c.kind, c.life, err)
		}
		if !c.ok {
			opWant(t, err, sqlstateCheckViolation, "a "+c.kind+" token living "+c.life)
		}
	}
	insCode := `INSERT INTO oauth_codes (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes, code_challenge, expires_at)
	            VALUES ($1, $2, $3, $4, $5, '{timesheets:read}', $6, now() + $7::interval)`
	for _, c := range []struct {
		life string
		ok   bool
	}{{"60 seconds", true}, {"59 seconds", false}, {"61 seconds", false}} {
		err := try(insCode, w.tenant, randHash(t), w.client, w.owner, oauthGoodRedirect, strings.Repeat("b", 43), c.life)
		if c.ok && err != nil {
			t.Errorf("a code living %s was refused: %v", c.life, err)
		}
		if !c.ok {
			opWant(t, err, sqlstateCheckViolation, "a code living "+c.life)
		}
	}
	// Born before Malta's DST change of 2026-03-29 and expiring after it: 2160 hours is one instant,
	// 90 days is another in that zone (a day there is 23 hours once) -- the reason the CHECK says hours.
	var hoursEqDaysMalta, hoursEqDaysUTC bool
	if err := tx.QueryRow(ctx, `SELECT set_config('TimeZone', 'Europe/Malta', true) IS NOT NULL
	                              AND ('2026-03-01 12:00+00'::timestamptz + interval '90 days') = ('2026-03-01 12:00+00'::timestamptz + interval '2160 hours')`).Scan(&hoursEqDaysMalta); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT set_config('TimeZone', 'UTC', true) IS NOT NULL
	                              AND ('2026-03-01 12:00+00'::timestamptz + interval '90 days') = ('2026-03-01 12:00+00'::timestamptz + interval '2160 hours')`).Scan(&hoursEqDaysUTC); err != nil {
		t.Fatal(err)
	}
	if hoursEqDaysMalta || !hoursEqDaysUTC {
		t.Fatalf("PREMISE: 90 days equals 2160 hours in Europe/Malta %v (want false) and in UTC %v (want true)", hoursEqDaysMalta, hoursEqDaysUTC)
	}
	var dst uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, created_at, expires_at)
	                            VALUES ($1, $2, 'refresh', $3, '2026-03-01 12:00+00', '2026-03-01 12:00+00'::timestamptz + interval '2160 hours')
	                            RETURNING id`, w.tenant, grant, randHash(t)).Scan(&dst); err != nil {
		t.Fatalf("a refresh token born before the DST change: %v", err)
	}
	for _, zone := range []string{"Europe/Malta", "UTC"} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Exec(ctx, `SELECT set_config('TimeZone', $1, true)`, zone); err != nil {
			t.Fatal(err)
		}
		_, uerr := sp.Exec(ctx, `UPDATE oauth_tokens SET revoked_at = clock_timestamp() WHERE id = $1`, dst)
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			t.Fatal(rbErr)
		}
		if uerr != nil {
			t.Errorf("revoked_at written from a %s session was refused: %v", zone, uerr)
		}
	}
}

// TestOAuthChecks_ScopesAndShapes -- the closed scope set without webhooks:manage on every array
// (§8.2, §13), the access/refresh scope rule (§14.2), used_at on refresh tokens only, a grant's
// revocation both-or-neither with a closed reason set, the code challenge's 43 characters.
func TestOAuthChecks_ScopesAndShapes(t *testing.T) {
	ctx, tx := opTx(t)
	w := newOAuthWorld(t, ctx, tx)
	grant, _, access := w.grant(t, ctx, tx, []string{"timesheets:read"})
	refuse := func(what, sql string, args ...any) {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = sp.Exec(ctx, sql, args...)
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			t.Fatal(rbErr)
		}
		opWant(t, err, sqlstateCheckViolation, what)
	}
	tok := `INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, scopes, expires_at)
	        VALUES ($1, $2, $3, $4, $5, now() + CASE $3::text WHEN 'access' THEN interval '1 hour' ELSE interval '2160 hours' END)`
	refuse("a refresh token carrying scopes", tok, w.tenant, grant, "refresh", randHash(t), []string{"timesheets:read"})
	refuse("an access token without scopes", tok, w.tenant, grant, "access", randHash(t), nil)
	refuse("an access token with an empty scope set", tok, w.tenant, grant, "access", randHash(t), []string{})
	refuse("an access token with webhooks:manage", tok, w.tenant, grant, "access", randHash(t), []string{"webhooks:manage"})
	refuse("an access token with a NULL scope", tok, w.tenant, grant, "access", randHash(t), []*string{nil})
	refuse("a token of a third kind", tok, w.tenant, grant, "id_token", randHash(t), nil)
	refuse("used_at on an access token", `UPDATE oauth_tokens SET used_at = clock_timestamp() WHERE id = $1`, access)
	refuse("a grant with webhooks:manage", `INSERT INTO oauth_grants (tenant_id, client_id, scopes, created_by) VALUES ($1, $2, '{webhooks:manage}', $3)`, w.tenant, w.client, w.owner)
	refuse("a grant with no scope", `INSERT INTO oauth_grants (tenant_id, client_id, scopes, created_by) VALUES ($1, $2, '{}', $3)`, w.tenant, w.client, w.owner)
	refuse("a revocation without a reason", `UPDATE oauth_grants SET revoked_at = clock_timestamp() WHERE id = $1`, grant)
	refuse("a reason without a revocation", `UPDATE oauth_grants SET revoked_reason = 'owner' WHERE id = $1`, grant)
	refuse("a reason outside the set", `UPDATE oauth_grants SET revoked_at = clock_timestamp(), revoked_reason = 'expired' WHERE id = $1`, grant)
	refuse("an app allowed webhooks:manage", `INSERT INTO oauth_clients (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by)
	                                           VALUES ('X', $1, '{timesheets:read,webhooks:manage}', $2, 'tt_cs_live_AbCd', $3)`, []string{oauthGoodRedirect}, randHash(t), w.operator.id)
	refuse("a code with a short challenge", `INSERT INTO oauth_codes (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes, code_challenge)
	                                          VALUES ($1, $2, $3, $4, $5, '{timesheets:read}', $6)`, w.tenant, randHash(t), w.client, w.owner, oauthGoodRedirect, strings.Repeat("a", 42))
	// The CHECK lists name exactly the closed set (one copy per table, read from the catalogue).
	for _, c := range [][2]string{{"oauth_clients", "oauth_clients_allowed_scopes_shape"}, {"oauth_grants", "oauth_grants_scopes_shape"},
		{"oauth_codes", "oauth_codes_scopes_shape"}, {"oauth_tokens", "oauth_tokens_scopes_shape"}} {
		def, valid := opConstraint(t, ctx, tx, c[0], c[1])
		m := regexp.MustCompile(`'\{([^}]*)\}'::text\[\]`).FindStringSubmatch(def)
		if !valid || m == nil || !slices.Equal(strings.Split(m[1], ","), oauthScopeSet) {
			t.Errorf("%s names %q (validated %v), want exactly %v", c[1], def, valid, oauthScopeSet)
		}
	}
}

// TestOAuthTriggers_StateIsOneWay -- 00104's four triggers (00051's rule): a stamped disabled_at,
// revocation, consumption, use or token revocation never moves or clears (23001), while the guarded,
// idempotent COALESCE write the queries carry never reaches the trigger.
func TestOAuthTriggers_StateIsOneWay(t *testing.T) {
	ctx, tx := opTx(t)
	w := newOAuthWorld(t, ctx, tx)
	grant, refresh, _ := w.grant(t, ctx, tx, []string{"timesheets:read"})
	var code uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO oauth_codes (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes, code_challenge)
	                            VALUES ($1, $2, $3, $4, $5, '{timesheets:read}', $6) RETURNING id`,
		w.tenant, randHash(t), w.client, w.owner, oauthGoodRedirect, strings.Repeat("c", 43)).Scan(&code); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`,
		`UPDATE oauth_grants SET revoked_at = clock_timestamp(), revoked_reason = 'client' WHERE id = $1`,
		`UPDATE oauth_codes SET consumed_at = clock_timestamp(), grant_id = $2 WHERE id = $1`,
		`UPDATE oauth_tokens SET used_at = clock_timestamp(), revoked_at = clock_timestamp() WHERE id = $1`,
	} {
		args := []any{map[string]uuid.UUID{"oauth_clients": w.client, "oauth_grants": grant, "oauth_codes": code, "oauth_tokens": refresh}[strings.Fields(s)[1]]}
		if strings.Contains(s, "$2") {
			args = append(args, grant)
		}
		if _, err := tx.Exec(ctx, s, args...); err != nil {
			t.Fatalf("CONTROL: the first stamp: %s: %v", s, err)
		}
	}
	for _, c := range []struct {
		what, sql string
		args      []any
	}{
		{"re-opening an app", `UPDATE oauth_clients SET disabled_at = NULL WHERE id = $1`, []any{w.client}},
		{"moving an app's disabled_at", `UPDATE oauth_clients SET disabled_at = disabled_at + interval '1 second' WHERE id = $1`, []any{w.client}},
		{"un-revoking a grant", `UPDATE oauth_grants SET revoked_at = NULL, revoked_reason = NULL WHERE id = $1`, []any{grant}},
		{"changing a revocation's reason", `UPDATE oauth_grants SET revoked_reason = 'owner' WHERE id = $1`, []any{grant}},
		{"naming a revoker after the fact", `UPDATE oauth_grants SET revoked_by = $2 WHERE id = $1`, []any{grant, w.owner}},
		{"un-spending a code", `UPDATE oauth_codes SET consumed_at = NULL WHERE id = $1`, []any{code}},
		{"moving a code's grant", `UPDATE oauth_codes SET grant_id = NULL WHERE id = $1`, []any{code}},
		{"moving a token's first use", `UPDATE oauth_tokens SET used_at = used_at + interval '1 second' WHERE id = $1`, []any{refresh}},
		{"un-revoking a token", `UPDATE oauth_tokens SET revoked_at = NULL WHERE id = $1`, []any{refresh}},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = sp.Exec(ctx, c.sql, c.args...)
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			t.Fatal(rbErr)
		}
		opWant(t, err, sqlstateRestrictViolation, c.what)
	}
	// The queries' guarded forms meet a stamped row and write nothing -- no exception reaches them.
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		if n, err := q.RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{Reason: "owner", TenantID: w.tenant, ID: grant}); err != nil || n != 0 {
			t.Errorf("RevokeOAuthGrant on a revoked grant: %d rows, %v; want 0, nil", n, err)
		}
		if n, err := q.DisconnectOAuthGrant(ctx, store.DisconnectOAuthGrantParams{Actor: w.owner, ID: grant, TenantID: w.tenant}); err != nil || n != 0 {
			t.Errorf("DisconnectOAuthGrant on a revoked grant: %d rows, %v; want 0, nil", n, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Each trigger function keeps EXECUTE in its owner alone and is bound where it asserts.
	for _, fn := range []string{"oauth_clients_disabling_is_one_way()", "oauth_grants_revocation_is_one_way()",
		"oauth_codes_spending_is_one_way()", "oauth_tokens_state_is_one_way()"} {
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_roles r WHERE r.rolname IN ('public', 'tappa_app', 'tappa_operator', 'tappa_opdefiner', 'tappa_resolver')
		                            AND has_function_privilege(r.oid, ('public.' || $1)::regprocedure, 'EXECUTE')`, fn); n != 0 {
			t.Errorf("%s is EXECUTE to a role other than its owner", fn)
		}
	}
}

// --------------------------------------------------------- the code's spending --

// TestOAuthCode_ASequentialPresentationOfASpentCodeIsAReuse -- §5.2 steps 3-4 through the shipped
// ConsumeOAuthCode: the first presentation spends the code (reached, spent, the code's facts); after
// its grant is recorded, a second presentation reaches the free row and reads it consumed with that
// grant -- the reuse branch, whose RevokeOAuthGrant ('code_reuse') kills the grant and every token of
// it (the resolver then carries grant_revoked_at). An expired code is reached and not spent, with no
// grant to revoke.
func TestOAuthCode_ASequentialPresentationOfASpentCodeIsAReuse(t *testing.T) {
	ctx, tx := opTx(t)
	w := newOAuthWorld(t, ctx, tx)
	codeHash := randHash(t)
	var code uuid.UUID
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		r, err := q.InsertOAuthCode(ctx, store.InsertOAuthCodeParams{TenantID: w.tenant, CodeHash: codeHash, ClientID: w.client,
			CreatedBy: w.owner, RedirectUri: oauthGoodRedirect, Scopes: []string{"timesheets:read"}, CodeChallenge: strings.Repeat("d", 43)})
		code = r.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var grant, access uuid.UUID
	accessHash := randHash(t)
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		r, err := q.ConsumeOAuthCode(ctx, store.ConsumeOAuthCodeParams{CodeID: code, TenantID: w.tenant})
		if err != nil {
			return err
		}
		if !r.Reached || !r.Spent || r.ClientID == nil || *r.ClientID != w.client || r.RedirectUri == nil || *r.RedirectUri != oauthGoodRedirect ||
			r.CreatedBy == nil || *r.CreatedBy != w.owner || !slices.Equal(r.Scopes, []string{"timesheets:read"}) || r.PriorConsumedAt != nil {
			t.Fatalf("the first presentation: %+v; want reached and spent with the code's facts", r)
		}
		g, err := q.InsertOAuthGrant(ctx, store.InsertOAuthGrantParams{TenantID: w.tenant, ClientID: w.client, Scopes: r.Scopes, CreatedBy: w.owner})
		if err != nil {
			return err
		}
		grant = g.ID
		if n, err := q.SetOAuthCodeGrant(ctx, store.SetOAuthCodeGrantParams{GrantID: grant, TenantID: w.tenant, CodeID: code}); err != nil || n != 1 {
			t.Fatalf("SetOAuthCodeGrant: %d, %v", n, err)
		}
		a, err := q.InsertOAuthToken(ctx, store.InsertOAuthTokenParams{TenantID: w.tenant, GrantID: grant, Kind: "access", TokenHash: accessHash, Scopes: r.Scopes})
		access = a.ID
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		r, err := q.ConsumeOAuthCode(ctx, store.ConsumeOAuthCodeParams{CodeID: code, TenantID: w.tenant})
		if err != nil {
			return err
		}
		if !r.Reached || r.Spent || r.PriorConsumedAt == nil || r.PriorGrantID == nil || *r.PriorGrantID != grant {
			t.Fatalf("the second presentation: %+v; want reached, not spent, consumed with the grant", r)
		}
		if n, err := q.RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{Reason: "code_reuse", TenantID: w.tenant, ID: *r.PriorGrantID}); err != nil || n != 1 {
			t.Fatalf("revoke the grant the reused code bore: %d, %v", n, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var reason string
	if err := tx.QueryRow(ctx, `SELECT revoked_reason FROM oauth_grants WHERE id = $1`, grant).Scan(&reason); err != nil || reason != "code_reuse" {
		t.Errorf("the grant's revocation reads %q (%v), want code_reuse", reason, err)
	}
	var grantRevoked bool
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT grant_revoked_at IS NOT NULL FROM resolve_oauth_token_by_hash($1)`, accessHash).Scan(&grantRevoked)
	}); err != nil || !grantRevoked {
		t.Errorf("the reused code's access token resolves with grant_revoked_at set = %v (%v), want true", grantRevoked, err)
	}
	_ = access
	// An expired code (its row written by the owner, born 61 seconds ago): reached, not spent, nothing
	// to revoke.
	var stale uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO oauth_codes (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes, code_challenge, created_at, expires_at)
	                            VALUES ($1, $2, $3, $4, $5, '{timesheets:read}', $6, now() - interval '61 seconds', now() - interval '1 second')
	                            RETURNING id`, w.tenant, randHash(t), w.client, w.owner, oauthGoodRedirect, strings.Repeat("e", 43)).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		r, err := q.ConsumeOAuthCode(ctx, store.ConsumeOAuthCodeParams{CodeID: stale, TenantID: w.tenant})
		if err == nil && (!r.Reached || r.Spent || r.PriorConsumedAt != nil || r.PriorGrantID != nil) {
			t.Errorf("an expired code: %+v; want reached, not spent, never consumed", r)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Another tenant's code is not reached from this one (the explicit filter and RLS both).
	other := newOAuthWorld(t, ctx, tx)
	if err := oauthAs(t, ctx, tx, other.tenant, func(q *store.Queries, _ pgx.Tx) error {
		r, err := q.ConsumeOAuthCode(ctx, store.ConsumeOAuthCodeParams{CodeID: code, TenantID: other.tenant})
		if err == nil && (r.Reached || r.Spent) {
			t.Errorf("another tenant reached this tenant's code: %+v", r)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// ------------------------------------------------------- the refresh's window --

// TestOAuthRefresh_ReuseOutsideTheWindowRevokesTheGrant -- §5.3 steps 4-5 through the shipped
// GetOAuthRefreshTokenState: a refresh token first used 29 seconds ago is within the tolerance, one
// used 31 seconds ago is not (the database's clock decides) and its grant is revoked
// ('refresh_reuse'); a revoked refresh token is neither (invalid_grant, no grant revocation). The
// tolerance's RevokeOtherLiveOAuthTokens leaves the USED refresh tokens alone and kills every other
// live token.
func TestOAuthRefresh_ReuseOutsideTheWindowRevokesTheGrant(t *testing.T) {
	ctx, tx := opTx(t)
	w := newOAuthWorld(t, ctx, tx)
	grant, liveRefresh, liveAccess := w.grant(t, ctx, tx, []string{"timesheets:read"})
	used := func(ago string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, created_at, expires_at, used_at)
		                            VALUES ($1, $2, 'refresh', $3, now() - interval '1 hour', now() - interval '1 hour' + interval '2160 hours',
		                                    clock_timestamp() - $4::interval) RETURNING id`, w.tenant, grant, randHash(t), ago).Scan(&id); err != nil {
			t.Fatalf("a refresh token used %s ago: %v", ago, err)
		}
		return id
	}
	fresh, stale := used("29 seconds"), used("31 seconds")
	var revoked uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, expires_at, revoked_at)
	                            VALUES ($1, $2, 'refresh', $3, now() + interval '2160 hours', clock_timestamp()) RETURNING id`, w.tenant, grant, randHash(t)).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		if _, err := q.LockLiveOAuthGrant(ctx, store.LockLiveOAuthGrantParams{TenantID: w.tenant, GrantID: grant}); err != nil {
			return err
		}
		for _, c := range []struct {
			id     uuid.UUID
			within bool
			what   string
		}{{fresh, true, "used 29 s ago"}, {stale, false, "used 31 s ago"}, {revoked, false, "revoked"}} {
			if _, err := q.RotateOAuthRefreshToken(ctx, store.RotateOAuthRefreshTokenParams{TenantID: w.tenant, TokenID: c.id}); !errors.Is(err, pgx.ErrNoRows) {
				t.Errorf("rotating a refresh token %s: %v, want no row", c.what, err)
			}
			s, err := q.GetOAuthRefreshTokenState(ctx, store.GetOAuthRefreshTokenStateParams{TenantID: w.tenant, TokenID: c.id})
			if err != nil {
				return err
			}
			if s.WithinTolerance != c.within {
				t.Errorf("a refresh token %s: within the tolerance %v, want %v", c.what, s.WithinTolerance, c.within)
			}
		}
		// The tolerance's revocation: every other live token, never a used refresh token.
		n, err := q.RevokeOtherLiveOAuthTokens(ctx, store.RevokeOtherLiveOAuthTokensParams{TenantID: w.tenant, GrantID: grant})
		if err != nil {
			return err
		}
		if n != 2 {
			t.Errorf("the tolerance revoked %d tokens, want 2 (the live refresh and the live access)", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM oauth_tokens WHERE id IN ($1, $2) AND revoked_at IS NOT NULL`, liveRefresh, liveAccess); n != 2 {
		t.Errorf("%d of the live pair are revoked after the tolerance, want 2", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM oauth_tokens WHERE id IN ($1, $2) AND revoked_at IS NULL AND used_at IS NOT NULL`, fresh, stale); n != 2 {
		t.Errorf("%d of the two used refresh tokens are left alone by the tolerance, want 2", n)
	}
	// Past the window: the grant ends.
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		n, err := q.RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{Reason: "refresh_reuse", TenantID: w.tenant, ID: grant})
		if err == nil && n != 1 {
			t.Errorf("RevokeOAuthGrant(refresh_reuse): %d rows, want 1", n)
		}
		if _, err := q.LockLiveOAuthGrant(ctx, store.LockLiveOAuthGrantParams{TenantID: w.tenant, GrantID: grant}); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("the revoked grant still locks as live: %v", err)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// ------------------------------------------------------------- the resolvers --

// TestOAuthResolvers_SecurityDefinerProperties -- ADR 0002 md.7's five constraints on the three
// resolvers (ADR 0047 §14.3), measured: owner tappa_resolver, SECURITY DEFINER, STABLE, search_path
// pinned, the exact arguments and result columns, EXECUTE to tappa_app and not PUBLIC, at most one row
// (a known key one row, an unknown none), every state carried and none applied; resolve_oauth_client's
// secret comparison in the function (true, false, NULL without a hash) and no hash in any result.
func TestOAuthResolvers_SecurityDefinerProperties(t *testing.T) {
	ctx, tx := opTx(t)
	want := map[string][2]string{
		"resolve_oauth_client": {"p_client_id uuid, p_secret_hash text",
			"TABLE(id uuid, name text, redirect_uris text[], allowed_scopes text[], disabled_at timestamp with time zone, secret_ok boolean)"},
		"resolve_oauth_code_by_hash": {"p_code_hash text",
			"TABLE(tenant_id uuid, code_id uuid, client_id uuid, consumed_at timestamp with time zone, expires_at timestamp with time zone, grant_id uuid)"},
		"resolve_oauth_token_by_hash": {"p_token_hash text",
			"TABLE(tenant_id uuid, token_id uuid, kind text, grant_id uuid, client_id uuid, token_scopes text[], expires_at timestamp with time zone, used_at timestamp with time zone, token_revoked_at timestamp with time zone, grant_revoked_at timestamp with time zone, client_disabled_at timestamp with time zone, api_enabled_at timestamp with time zone, api_code text)"},
	}
	for fn, w := range want {
		var owner, args, result, vol string
		var definer, public, app bool
		var config []string
		if err := tx.QueryRow(ctx, `
			SELECT pg_get_userbyid(p.proowner), p.prosecdef, p.provolatile::text, p.proconfig,
			       pg_get_function_arguments(p.oid), pg_get_function_result(p.oid),
			       has_function_privilege('public', p.oid, 'EXECUTE'), has_function_privilege('tappa_app', p.oid, 'EXECUTE')
			  FROM pg_proc p WHERE p.proname = $1 AND p.pronamespace = 'public'::regnamespace`, fn).Scan(
			&owner, &definer, &vol, &config, &args, &result, &public, &app); err != nil {
			t.Fatalf("%s: %v (one function of that name expected)", fn, err)
		}
		if owner != "tappa_resolver" || !definer || vol != "s" || !slices.Equal(config, []string{"search_path=pg_catalog, pg_temp"}) ||
			args != w[0] || result != w[1] || public || !app {
			t.Errorf("%s: owner %s definer %v volatile %s config %v args %q result %q PUBLIC %v tappa_app %v", fn, owner, definer, vol, config, args, result, public, app)
		}
		if strings.Contains(strings.ToLower(result), "hash") {
			t.Errorf("%s returns a hash column: %s", fn, result)
		}
	}
	w := newOAuthWorld(t, ctx, tx)
	grant, _, _ := w.grant(t, ctx, tx, []string{"timesheets:read", "employees:write"})
	tokenHash, codeHash, secret := randHash(t), randHash(t), randHash(t)
	if _, err := tx.Exec(ctx, `UPDATE oauth_clients SET secret_hash = $2 WHERE id = $1`, w.client, secret); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, scopes, expires_at)
	                           VALUES ($1, $2, 'access', $3, '{timesheets:read}', now() + interval '1 hour')`, w.tenant, grant, tokenHash); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_codes (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes, code_challenge)
	                           VALUES ($1, $2, $3, $4, $5, '{timesheets:read}', $6)`, w.tenant, codeHash, w.client, w.owner, oauthGoodRedirect, strings.Repeat("f", 43)); err != nil {
		t.Fatal(err)
	}
	// The operator closed the business's API: api_code kept, api_enabled_at NULL (§11.4).
	if _, err := tx.Exec(ctx, `UPDATE tenants SET api_code = 'OAUTHX', api_enabled_at = NULL WHERE id = $1`, w.tenant); err != nil {
		t.Fatal(err)
	}
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		count := func(sql string, arg ...any) int64 {
			var n int64
			if err := sp.QueryRow(ctx, sql, arg...).Scan(&n); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
			return n
		}
		if count(`SELECT count(*) FROM resolve_oauth_token_by_hash($1)`, tokenHash) != 1 || count(`SELECT count(*) FROM resolve_oauth_token_by_hash($1)`, randHash(t)) != 0 ||
			count(`SELECT count(*) FROM resolve_oauth_code_by_hash($1)`, codeHash) != 1 || count(`SELECT count(*) FROM resolve_oauth_code_by_hash($1)`, randHash(t)) != 0 ||
			count(`SELECT count(*) FROM resolve_oauth_client($1, NULL)`, w.client) != 1 || count(`SELECT count(*) FROM resolve_oauth_client($1, NULL)`, uuid.New()) != 0 {
			t.Error("a resolver did not answer one row for a known key and none for an unknown one")
		}
		var tenant, client uuid.UUID
		var kind string
		var scopes []string
		var apiEnabled, grantRevoked, disabled *string
		var code *string
		if err := sp.QueryRow(ctx, `SELECT tenant_id, client_id, kind, token_scopes, api_enabled_at::text, api_code, grant_revoked_at::text, client_disabled_at::text
		                             FROM resolve_oauth_token_by_hash($1)`, tokenHash).Scan(&tenant, &client, &kind, &scopes, &apiEnabled, &code, &grantRevoked, &disabled); err != nil {
			return err
		}
		if tenant != w.tenant || client != w.client || kind != "access" || !slices.Equal(scopes, []string{"timesheets:read"}) ||
			apiEnabled != nil || code == nil || *code != "OAUTHX" || grantRevoked != nil || disabled != nil {
			t.Errorf("the token resolver carries tenant %v client %v kind %s scopes %v (the TOKEN's, not the grant's) api_enabled_at %v api_code %v", tenant, client, kind, scopes, apiEnabled, code)
		}
		for _, c := range []struct {
			hash *string
			want *bool
		}{{&secret, boolPtr(true)}, {strPtr(randHash(t)), boolPtr(false)}, {nil, nil}} {
			var ok *bool
			if err := sp.QueryRow(ctx, `SELECT secret_ok FROM resolve_oauth_client($1, $2)`, w.client, c.hash).Scan(&ok); err != nil {
				return err
			}
			if (ok == nil) != (c.want == nil) || (ok != nil && *ok != *c.want) {
				t.Errorf("secret_ok %v, want %v", ok, c.want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// The Go accessors' statements are the resolvers' columns, in order (the hand-written mirror).
	for _, s := range []string{getOAuthClientSQL, getOAuthCodeByHashSQL, getOAuthTokenByHashSQL} {
		if strings.Contains(s, "*") || strings.Contains(s, "hash,") {
			t.Errorf("a resolver statement reads more than its fixed columns: %s", s)
		}
	}
}

func boolPtr(b bool) *bool { return &b }

// ------------------------------------------------------- the K-22 writer pin --

// TestOAuth_APICodeHasOneWriter -- ADR 0047 §11.4's inference ("closed by the operator" is
// api_enabled_at NULL with api_code set) rests on api_code being written only when the API is opened.
// A grant test alone cannot hold it -- tappa_opdefiner holds UPDATE (api_code) for op_enable_tenant_api
// (00039) -- so every function body is scanned (prosrc): the one that writes api_code to tenants is
// op_enable_tenant_api; and no role but the definer may UPDATE or INSERT the column.
func TestOAuth_APICodeHasOneWriter(t *testing.T) {
	ctx, tx := opTx(t)
	rows, err := tx.Query(ctx, `SELECT p.proname, p.prosrc FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
	                             WHERE n.nspname = 'public' AND p.prosrc ~* 'api_code' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	writes := regexp.MustCompile(`(?is)\bupdate\s+(?:public\.)?tenants\b[^;]*?\bset\b[^;]*?\bapi_code\s*=|\binsert\s+into\s+(?:public\.)?tenants\b[^;]*?\bapi_code\b`)
	var writers, readers []string
	for rows.Next() {
		var name, src string
		if err := rows.Scan(&name, &src); err != nil {
			t.Fatal(err)
		}
		readers = append(readers, name)
		if writes.MatchString(src) {
			writers = append(writers, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(readers) < 3 {
		t.Fatalf("anti-vacuity: %d functions name api_code (%v)", len(readers), readers)
	}
	if !slices.Equal(writers, []string{"op_enable_tenant_api"}) {
		t.Errorf("the functions that write tenants.api_code are %v, want exactly [op_enable_tenant_api] (readers: %v)", writers, readers)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_roles r
	                            WHERE r.rolname NOT IN ('tappa_opdefiner') AND NOT r.rolsuper AND r.rolname !~ '^pg_'
	                              AND (has_column_privilege(r.oid, 'public.tenants', 'api_code', 'UPDATE')
	                                   OR has_column_privilege(r.oid, 'public.tenants', 'api_code', 'INSERT'))`); n != 0 {
		t.Errorf("%d roles besides tappa_opdefiner may write tenants.api_code", n)
	}
}

// --------------------------------------------------- the redirect rule and op_* --

// TestOAuth_TheRedirectRuleIsOneExpression -- ADR 0047 §10.2 in SQL: one fixed expression in the
// table CHECK and in both writers' bodies (each copy byte for byte), and its behaviour -- the writers
// refuse http://, an IP literal, a port other than 443, a query, a fragment, userinfo, an upper-case
// host, a bare host without a path, six addresses, an address over 512 bytes, a repeat and a NULL with
// 22023 and write nothing; the table refuses the same addresses with 23514; the loopback form passes
// (its build rule is Go's).
func TestOAuth_TheRedirectRuleIsOneExpression(t *testing.T) {
	ctx, tx := opTx(t)
	def, valid := opConstraint(t, ctx, tx, "oauth_clients", "oauth_clients_redirect_uris_shape")
	if n := strings.Count(def, "'"+oauthRedirectRule+"'::text"); !valid || n != 5 {
		t.Errorf("oauth_clients_redirect_uris_shape carries the rule %d times (validated %v), want 5 -- one per slot: %s", n, valid, def)
	}
	if exprs := regexp.MustCompile(`~ '([^']*)'`).FindAllStringSubmatch(def, -1); len(exprs) != 5 {
		t.Errorf("the CHECK carries %d regular expressions, want the one rule five times", len(exprs))
	}
	for _, fn := range []string{"op_register_oauth_client", "op_set_oauth_client_redirects"} {
		var src string
		if err := tx.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE proname = $1`, fn).Scan(&src); err != nil {
			t.Fatal(err)
		}
		if strings.Count(src, "!~ '"+oauthRedirectRule+"'") != 1 || strings.Count(src, "octet_length(u.v) > 512") != 1 ||
			strings.Count(src, "cardinality(p_redirect_uris) NOT BETWEEN 1 AND 5") != 1 {
			t.Errorf("%s does not test the rule, the 512-byte bound and the 1..5 count exactly once each", fn)
		}
	}
	w := newOAuthWorld(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, w.operator.id, true)
	long := "https://a.example/" + strings.Repeat("x", 512-len("https://a.example/")+1)
	bad := map[string][]string{
		"http":             {"http://maltacalculator.com/cb"},
		"an IP literal":    {"https://10.0.0.1/cb"},
		"an IPv6 literal":  {"https://[::1]/cb"},
		"port 8443":        {"https://maltacalculator.com:8443/cb"},
		"a query":          {"https://maltacalculator.com/cb?x=1"},
		"a fragment":       {"https://maltacalculator.com/cb#f"},
		"userinfo":         {"https://user@maltacalculator.com/cb"},
		"an upper case":    {"https://MaltaCalculator.com/cb"},
		"no path":          {"https://maltacalculator.com"},
		"a dotless host":   {"https://localhost/cb"},
		"loopback no port": {"http://localhost/cb"},
		"six addresses": {"https://a.example/1", "https://a.example/2", "https://a.example/3", "https://a.example/4",
			"https://a.example/5", "https://a.example/6"},
		"513 bytes": {long},
		"a repeat":  {oauthGoodRedirect, oauthGoodRedirect},
		"none":      {},
	}
	before := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind LIKE 'oauth_client_%'`)
	for what, uris := range bad {
		err := opExecAs(t, ctx, tx, "tappa_operator", registerOAuthClientSQL, hash, "Bad "+what, uris, []string{"timesheets:read"}, randHash(t), "tt_cs_live_AbCd")
		opWantClean(t, err, sqlstateInvalidParameter, "op_register_oauth_client: change refused", "register with "+what)
		err = opExecAs(t, ctx, tx, "tappa_operator", setOAuthClientRedirectsSQL, hash, w.client, uris)
		opWantClean(t, err, sqlstateInvalidParameter, "op_set_oauth_client_redirects: change refused", "set redirects with "+what)
		if what == "none" || what == "a repeat" || what == "six addresses" {
			continue
		}
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = sp.Exec(ctx, `UPDATE oauth_clients SET redirect_uris = $2 WHERE id = $1`, w.client, uris)
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			t.Fatal(rbErr)
		}
		opWant(t, err, sqlstateCheckViolation, "the table CHECK on "+what)
	}
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", setOAuthClientRedirectsSQL, hash, w.client, []*string{nil}), sqlstateInvalidParameter, "set redirects with a NULL address")
	if after := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind LIKE 'oauth_client_%'`); after != before {
		t.Errorf("refused calls wrote %d audit rows", after-before)
	}
	for _, uris := range [][]string{{"http://localhost:3000/api/integrations/taptime/callback"}, {"http://127.0.0.1:8080/cb", "https://maltacalculator.com:443/cb"}} {
		if err := opExecAs(t, ctx, tx, "tappa_operator", setOAuthClientRedirectsSQL, hash, w.client, uris); err != nil {
			t.Errorf("set redirects %v: %v", uris, err)
		}
	}
	var got []string
	if err := tx.QueryRow(ctx, `SELECT redirect_uris FROM oauth_clients WHERE id = $1`, w.client).Scan(&got); err != nil || len(got) != 2 {
		t.Errorf("the loopback list was not stored: %v (%v)", got, err)
	}
}

// TestOAuthOp_EveryWriteLeavesItsAuditRow -- ADR 0047 §10.2 / ADR 0021 §2 through the shipped Go
// accessors (internal/db/operator.go): registration returns the id, stores the scopes sorted and
// without repeats and writes 'oauth_client_registered' naming the app and no tenant; rotation, the
// redirect edit and the disabling each write their row on EVERY accepted call -- also for a disabled
// app (which none of them changes) and for an id that names no app; webhooks:manage, a name the rule
// refuses and a bad hash are 22023 with no row; a dead session is ErrOperatorRefused; the two-phase
// read returns the registry without the hash and writes the 'read' row with scope 'oauth_clients'.
func TestOAuthOp_EveryWriteLeavesItsAuditRow(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	auditRows := func(kind string) []string {
		t.Helper()
		rows, err := tx.Query(ctx, `SELECT detail->>'client_id' FROM operator_audit_log
		                             WHERE kind = $1 AND session_id = $2 AND actor_admin_id = $3 AND target_tenant_id IS NULL
		                               AND target_admin_id IS NULL AND target_scope IS NULL ORDER BY at, id`, kind, session, a.id)
		if err != nil {
			t.Fatal(err)
		}
		out, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	var id uuid.UUID
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var err error
		id, err = RegisterOAuthClient(ctx, sp, hash, "Malta Calculator", []string{oauthGoodRedirect},
			[]string{"employees:write", "timesheets:read", "employees:write"}, randHash(t), "tt_cs_live_Ab_d")
		return err
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	var scopes []string
	var createdBy uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT allowed_scopes, created_by FROM oauth_clients WHERE id = $1`, id).Scan(&scopes, &createdBy); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(scopes, []string{"employees:write", "timesheets:read"}) || createdBy != a.id {
		t.Errorf("the registered app holds scopes %v created by %v, want [employees:write timesheets:read] by the session's operator", scopes, createdBy)
	}
	if got := auditRows("oauth_client_registered"); !slices.Equal(got, []string{id.String()}) {
		t.Errorf("registration rows %v, want one naming %s", got, id)
	}
	var before string
	if err := tx.QueryRow(ctx, `SELECT secret_hint FROM oauth_clients WHERE id = $1`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	ghost := uuid.New()
	op := func(what string, fn func(sp pgx.Tx) error) {
		t.Helper()
		if err := opAs(t, ctx, tx, "tappa_operator", fn); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	op("rotate", func(sp pgx.Tx) error {
		return RotateOAuthClientSecret(ctx, sp, hash, id, randHash(t), "tt_cs_live_WxYz")
	})
	op("set redirects", func(sp pgx.Tx) error {
		return SetOAuthClientRedirects(ctx, sp, hash, id, []string{oauthGoodRedirect, "https://maltacalculator.com/b"})
	})
	op("disable", func(sp pgx.Tx) error { return DisableOAuthClient(ctx, sp, hash, id) })
	var hint string
	var rotated, disabled bool
	var uris []string
	if err := tx.QueryRow(ctx, `SELECT secret_hint, secret_rotated_at IS NOT NULL, disabled_at IS NOT NULL, redirect_uris FROM oauth_clients WHERE id = $1`, id).Scan(&hint, &rotated, &disabled, &uris); err != nil {
		t.Fatal(err)
	}
	if hint != "tt_cs_live_WxYz" || !rotated || !disabled || len(uris) != 2 {
		t.Errorf("after the three edits: hint %s rotated %v disabled %v uris %v", hint, rotated, disabled, uris)
	}
	// On the disabled app and on an id naming nothing: the same nil, nothing changes, the row is written.
	op("rotate a disabled app", func(sp pgx.Tx) error {
		return RotateOAuthClientSecret(ctx, sp, hash, id, randHash(t), "tt_cs_live_Zzzz")
	})
	op("edit a disabled app", func(sp pgx.Tx) error { return SetOAuthClientRedirects(ctx, sp, hash, id, []string{oauthGoodRedirect}) })
	op("disable again", func(sp pgx.Tx) error { return DisableOAuthClient(ctx, sp, hash, id) })
	op("rotate a ghost", func(sp pgx.Tx) error {
		return RotateOAuthClientSecret(ctx, sp, hash, ghost, randHash(t), "tt_cs_live_Gggg")
	})
	op("disable a ghost", func(sp pgx.Tx) error { return DisableOAuthClient(ctx, sp, hash, ghost) })
	if err := tx.QueryRow(ctx, `SELECT secret_hint, redirect_uris FROM oauth_clients WHERE id = $1`, id).Scan(&hint, &uris); err != nil || hint != "tt_cs_live_WxYz" || len(uris) != 2 {
		t.Errorf("a disabled app changed: hint %s uris %v (%v)", hint, uris, err)
	}
	for kind, want := range map[string][]string{
		"oauth_client_secret_rotated":    {id.String(), id.String(), ghost.String()},
		"oauth_client_redirects_changed": {id.String(), id.String()},
		"oauth_client_disabled":          {id.String(), id.String(), ghost.String()},
	} {
		if got := auditRows(kind); !slices.Equal(got, want) {
			t.Errorf("%s rows %v, want %v", kind, got, want)
		}
	}
	// Refusals: 22023, no row.
	n0 := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE session_id = $1`, session)
	for what, call := range map[string]func(sp pgx.Tx) error{
		"webhooks:manage": func(sp pgx.Tx) error {
			_, err := RegisterOAuthClient(ctx, sp, hash, "X", []string{oauthGoodRedirect}, []string{"webhooks:manage"}, randHash(t), "tt_cs_live_AbCd")
			return err
		},
		"no scope": func(sp pgx.Tx) error {
			_, err := RegisterOAuthClient(ctx, sp, hash, "X", []string{oauthGoodRedirect}, []string{}, randHash(t), "tt_cs_live_AbCd")
			return err
		},
		"a padded name": func(sp pgx.Tx) error {
			_, err := RegisterOAuthClient(ctx, sp, hash, " X", []string{oauthGoodRedirect}, []string{"timesheets:read"}, randHash(t), "tt_cs_live_AbCd")
			return err
		},
		"a bidi override in the name": func(sp pgx.Tx) error {
			_, err := RegisterOAuthClient(ctx, sp, hash, "Malta\u202eCalc", []string{oauthGoodRedirect}, []string{"timesheets:read"}, randHash(t), "tt_cs_live_AbCd")
			return err
		},
		"a 65-character name": func(sp pgx.Tx) error {
			_, err := RegisterOAuthClient(ctx, sp, hash, strings.Repeat("n", 65), []string{oauthGoodRedirect}, []string{"timesheets:read"}, randHash(t), "tt_cs_live_AbCd")
			return err
		},
		"an upper-case hash": func(sp pgx.Tx) error {
			return RotateOAuthClientSecret(ctx, sp, hash, id, strings.ToUpper(randHash(t)), "tt_cs_live_AbCd")
		},
		"a hint of another prefix": func(sp pgx.Tx) error {
			return RotateOAuthClientSecret(ctx, sp, hash, id, randHash(t), "tt_live_AbCd")
		},
	} {
		err := opAs(t, ctx, tx, "tappa_operator", call)
		if err == nil || !strings.Contains(err.Error(), "SQLSTATE 22023") || strings.Contains(err.Error(), hash) {
			t.Errorf("%s: %v, want a database error carrying 22023 and nothing of the session", what, err)
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE session_id = $1`, session); n != n0 {
		t.Errorf("refused calls wrote %d rows", n-n0)
	}
	// A colliding secret hash (another app's) is caught: 22023, no row (the integrity catch).
	var taken string
	if err := tx.QueryRow(ctx, `SELECT secret_hash FROM oauth_clients WHERE id = $1`, id).Scan(&taken); err != nil {
		t.Fatal(err)
	}
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		_, err := RegisterOAuthClient(ctx, sp, hash, "Twin", []string{oauthGoodRedirect}, []string{"timesheets:read"}, taken, "tt_cs_live_AbCd")
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "SQLSTATE 22023") || strings.Contains(err.Error(), taken) {
		t.Errorf("a colliding secret hash: %v, want 22023 without the hash", err)
	}
	// A dead session: ErrOperatorRefused.
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error { return DisableOAuthClient(ctx, sp, opRandHex(t), id) }); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("a dead session: %v, want ErrOperatorRefused", err)
	}
	// The read, phase two over a forged committed ticket (the two phases are two transactions in the
	// product; opForgeRead is the precedent's stand-in).
	raw := opRandHex(t)
	opForgeRead(t, ctx, tx, session, a.id, oauthClientsReadKind, opTicketHash(raw, 1, OAuthClientsPerPage), nil, opCommittedXact(t, ctx), "30 seconds")
	var got []OAuthClientRow
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, readOAuthClientsSQL, hash, raw, int32(1), int32(OAuthClientsPerPage))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r OAuthClientRow
			if err := rows.Scan(&r.ClientID, &r.Name, &r.RedirectURIs, &r.AllowedScopes, &r.SecretHint, &r.SecretRotatedAt,
				&r.CreatedAt, &r.CreatedBy, &r.CreatedByName, &r.DisabledAt); err != nil {
				return err
			}
			got = append(got, r)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("op_read_oauth_clients: %v", err)
	}
	found := false
	for _, r := range got {
		if r.ClientID == id {
			found = r.Name == "Malta Calculator" && r.SecretHint == "tt_cs_live_WxYz" && r.DisabledAt != nil && r.CreatedByName != nil
		}
	}
	if !found {
		t.Errorf("the registry read did not return the app as stored (%d rows)", len(got))
	}
	if _, err := opBegin(t, ctx, tx, hash, oauthClientsReadKind, `{"page_number": 1, "page_size": 200}`); err != nil {
		t.Errorf("op_begin_read 'oauth_clients': %v", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND target_scope = 'oauth_clients' AND session_id = $1
	                            AND target_tenant_id IS NULL AND page_number = 1 AND page_size = 200 AND detail = '{}'`, session); n != 1 {
		t.Errorf("op_begin_read 'oauth_clients' wrote %d read rows of the expected shape, want 1", n)
	}
	// The registry's shape CHECK refuses an app row naming a tenant.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = sp.Exec(ctx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, detail)
	                       VALUES ('oauth_client_disabled', $1, $2, gen_random_uuid(), jsonb_build_object('client_id', $3::text))`, session, a.id, id.String())
	if rbErr := sp.Rollback(ctx); rbErr != nil {
		t.Fatal(rbErr)
	}
	opWant(t, err, sqlstateCheckViolation, "an app row naming a tenant")
}

// TestOperator00107_TheBodiesAre00063sWithTheirEdits -- 00107 replaces op_begin_read and op_read_audit
// whole: undoing its edits gives 00063's Up bodies byte for byte, its Down gives those bodies back
// verbatim, and the live bodies are 00107's (the newest).
func TestOperator00107_TheBodiesAre00063sWithTheirEdits(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, "00107_register_oauth_clients_from_the_operator.sql")
	up63, _ := opMigrationSections(t, op00063File)
	kinds := "'oauth_client_registered',\n"
	edits := map[string][][2]string{
		"op_begin_read": {
			{"'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings') THEN", "'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings', 'oauth_clients') THEN"},
			{"                                                   'tenant_reinstated') THEN",
				"                                                   'tenant_reinstated', " + kinds +
					"                                                   'oauth_client_secret_rotated',\n" +
					"                                                   'oauth_client_redirects_changed',\n" +
					"                                                   'oauth_client_disabled') THEN"},
		},
		"op_read_audit": {
			{"                                                            'tenant_reinstated')\n",
				"                                                            'tenant_reinstated', " + kinds +
					"                                                            'oauth_client_secret_rotated',\n" +
					"                                                            'oauth_client_redirects_changed',\n" +
					"                                                            'oauth_client_disabled')\n"},
			{"                                                'tenant_vat', 'holdings')\n                            AND p.detail = '{}'::jsonb",
				"                                                'tenant_vat', 'holdings', 'oauth_clients')\n                            AND p.detail = '{}'::jsonb"},
			{"                       ELSE p.target_scope IS NULL AND p.detail = '{}'::jsonb",
				"                       WHEN p.kind IN ('oauth_client_registered', 'oauth_client_secret_rotated',\n" +
					"                                       'oauth_client_redirects_changed', 'oauth_client_disabled') THEN\n" +
					"                            p.target_scope IS NULL\n" +
					"                            AND p.detail ? 'client_id' AND (p.detail - 'client_id') = '{}'::jsonb\n" +
					"                            AND jsonb_typeof(p.detail -> 'client_id') = 'string'\n" +
					"                            AND (p.detail ->> 'client_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'\n" +
					"                       ELSE p.target_scope IS NULL AND p.detail = '{}'::jsonb"},
			{"                                            'tenant_vat', 'holdings')\n                    THEN s.target_scope END,",
				"                                            'tenant_vat', 'holdings', 'oauth_clients')\n                    THEN s.target_scope END,"},
		},
	}
	for fn, e := range edits {
		b107, _ := opBodyOf(t, up, "CREATE OR REPLACE FUNCTION", fn)
		b63, _ := opBodyOf(t, up63, "CREATE OR REPLACE FUNCTION", fn)
		bDown, _ := opBodyOf(t, down, "CREATE OR REPLACE FUNCTION", fn)
		if undone, missing := opUndo(b107, e); len(missing) > 0 || undone != b63 {
			t.Errorf("00107's %s is not 00063's body with its edits (edits not held exactly once: %v)", fn, missing)
		}
		if bDown != b63 {
			t.Errorf("00107's Down does not give %s back as 00063's Up body", fn)
		}
		var live string
		if err := tx.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE proname = $1 AND pronamespace = 'public'::regnamespace`, fn).Scan(&live); err != nil {
			t.Fatal(err)
		}
		if live != b107 || live != opNewestUpBody(t, fn) {
			t.Errorf("the live %s is not 00107's body (or 00107 is not its newest definition)", fn)
		}
	}
	for _, c := range []struct{ table, name, want string }{
		{"operator_read_tickets", "operator_read_tickets_kind_check", opKindCheckDef(opKindsAtHead)},
		{"operator_audit_log", "operator_audit_log_kind_check", opKindCheckDef(opAuditKinds)},
	} {
		if def, valid := opConstraint(t, ctx, tx, c.table, c.name); def != c.want || !valid {
			t.Errorf("%s is %s (validated %v),\n want %s, validated", c.name, def, valid, c.want)
		}
	}
	shape, _ := opConstraint(t, ctx, tx, "operator_audit_log", "operator_audit_log_oauth_client_shape")
	if lists := opQuotedArrays(shape); len(lists) != 1 || !slices.Equal(lists[0], opOAuthClientKinds) {
		t.Errorf("operator_audit_log_oauth_client_shape names %v, want exactly %v", lists, opOAuthClientKinds)
	}
}

// TestOperator00107_DownGivesBack00106AndUpTakesItAgain -- opAtVersion(106) walks 00107's Down inside
// the test's transaction: the five op_* go, op_begin_read and op_read_audit read 00063's bodies, the
// two kind CHECKs are 00063's and 00042's sets, the shape CHECK and the key to platform_admins go,
// tappa_opdefiner reaches nothing of oauth_clients -- and the registry's rows stay. Its Up then takes
// it all again.
func TestOperator00107_DownGivesBack00106AndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	opAtVersion(t, ctx, tx, 106, opKindsAt42...)
	up, _ := opMigrationSections(t, "00107_register_oauth_clients_from_the_operator.sql")
	state := func(when string, at107 bool) {
		t.Helper()
		fns := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname IN ('op_register_oauth_client', 'op_rotate_oauth_client_secret',
		                           'op_set_oauth_client_redirects', 'op_disable_oauth_client', 'op_read_oauth_clients')`)
		fk := opInt(t, ctx, tx, `SELECT count(*) FROM pg_constraint WHERE conname = 'oauth_clients_creator_fk'`)
		shape := opInt(t, ctx, tx, `SELECT count(*) FROM pg_constraint WHERE conname = 'operator_audit_log_oauth_client_shape'`)
		reach := opInt(t, ctx, tx, `SELECT count(*) WHERE has_any_column_privilege('tappa_opdefiner', 'public.oauth_clients', 'SELECT')
		                             OR has_any_column_privilege('tappa_opdefiner', 'public.oauth_clients', 'INSERT')
		                             OR has_any_column_privilege('tappa_opdefiner', 'public.oauth_clients', 'UPDATE')`)
		kinds, _ := opConstraint(t, ctx, tx, "operator_audit_log", "operator_audit_log_kind_check")
		tickets, _ := opConstraint(t, ctx, tx, "operator_read_tickets", "operator_read_tickets_kind_check")
		var begin, audit string
		if err := tx.QueryRow(ctx, `SELECT (SELECT md5(prosrc) FROM pg_proc WHERE proname = 'op_begin_read'), (SELECT md5(prosrc) FROM pg_proc WHERE proname = 'op_read_audit')`).Scan(&begin, &audit); err != nil {
			t.Fatal(err)
		}
		if at107 {
			if fns != 5 || fk != 1 || shape != 1 || reach != 1 || kinds != opKindCheckDef(opAuditKinds) || tickets != opKindCheckDef(opKindsAtHead) ||
				begin == "0f57e3a5444bdefad336f2fdb877d984" || audit == "75df477d8d45c986b7e4943e7051ec18" {
				t.Errorf("%s: functions %d key %d shape %d reach %d\n kinds %s\n tickets %s\n bodies %s %s; want 00107's state", when, fns, fk, shape, reach, kinds, tickets, begin, audit)
			}
			return
		}
		if fns != 0 || fk != 0 || shape != 0 || reach != 0 || kinds != opKindCheckDef(opAuditKinds63) || tickets != opKindCheckDef(opKindsAt42) ||
			begin != "0f57e3a5444bdefad336f2fdb877d984" || audit != "75df477d8d45c986b7e4943e7051ec18" {
			t.Errorf("%s: functions %d key %d shape %d reach %d\n kinds %s\n tickets %s\n bodies %s %s; want 00106's state", when, fns, fk, shape, reach, kinds, tickets, begin, audit)
		}
	}
	state("after 00107's Down", false)
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class WHERE relname = 'oauth_clients'`); n != 1 {
		t.Fatal("00107's Down took the registry away; it drops no table")
	}
	opRunSection(t, ctx, tx, up, "00107 Up again")
	state("after the Up again", true)
}

// ------------------------------------------------------------ api_idempotency --

// TestApiIdempotency00106_TheGrantNamespace -- ADR 0047 §14.5 and its "Uygulama notları": a grant's
// claim through the shipped ClaimIdempotencyForGrant conflicts with its own second claim (the ON
// CONFLICT names the partial index's predicate -- without it PostgreSQL infers no index, 42P10) and
// never with a key's claim of the same Idempotency-Key; the key's statements keep working; a row names
// exactly one principal; a grant of another tenant is refused by the composite key; the tenant-wide
// lazy delete sweeps a grant row as it sweeps a key row; 00106's Down deletes the grant rows and gives
// 00052's primary key back, and its Up takes the namespace again.
func TestApiIdempotency00106_TheGrantNamespace(t *testing.T) {
	ctx, tx := opTx(t)
	w, other := newOAuthWorld(t, ctx, tx), newOAuthWorld(t, ctx, tx)
	grant, _, _ := w.grant(t, ctx, tx, []string{"employees:write"})
	key := w.addKey(t, ctx, tx, "K", randHash(t))
	hash := randHash(t)
	created := int16(201)
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		for i, want := range []int64{1, 0} {
			n, err := q.ClaimIdempotencyForGrant(ctx, store.ClaimIdempotencyForGrantParams{TenantID: w.tenant, GrantID: grant, IdemKey: "order-1", RequestHash: hash})
			if err != nil {
				return err
			}
			if n != want {
				t.Errorf("the grant's claim #%d: %d rows, want %d", i+1, n, want)
			}
		}
		if n, err := q.ClaimIdempotency(ctx, store.ClaimIdempotencyParams{TenantID: w.tenant, KeyID: key, IdemKey: "order-1", RequestHash: hash}); err != nil || n != 1 {
			t.Errorf("a key's claim of the same Idempotency-Key: %d rows (%v), want 1 -- two namespaces", n, err)
		}
		if n, err := q.RecordIdempotencyForGrant(ctx, store.RecordIdempotencyForGrantParams{Status: &created, ResultRef: &grant, TenantID: w.tenant, GrantID: grant, IdemKey: "order-1"}); err != nil || n != 1 {
			t.Errorf("RecordIdempotencyForGrant: %d (%v)", n, err)
		}
		row, err := q.GetIdempotencyForGrant(ctx, store.GetIdempotencyForGrantParams{TenantID: w.tenant, GrantID: grant, IdemKey: "order-1"})
		if err != nil || row.RequestHash != hash || row.Status == nil || *row.Status != 201 {
			t.Errorf("GetIdempotencyForGrant: %+v (%v)", row, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	try := func(what, code, sql string, args ...any) {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = sp.Exec(ctx, sql, args...)
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			t.Fatal(rbErr)
		}
		opWant(t, err, code, what)
	}
	try("a row naming both principals", sqlstateCheckViolation,
		`INSERT INTO api_idempotency (tenant_id, key_id, grant_id, idem_key, request_hash) VALUES ($1, $2, $3, 'x', $4)`, w.tenant, key, grant, hash)
	try("a row naming neither", sqlstateCheckViolation,
		`INSERT INTO api_idempotency (tenant_id, idem_key, request_hash) VALUES ($1, 'x', $2)`, w.tenant, hash)
	try("a claim naming another tenant's grant", sqlstateForeignKeyViolation,
		`INSERT INTO api_idempotency (tenant_id, grant_id, idem_key, request_hash) VALUES ($1, $2, 'x', $3)`, other.tenant, grant, hash)
	// The tenant-wide lazy delete reaches both namespaces.
	if _, err := tx.Exec(ctx, `UPDATE api_idempotency SET created_at = now() - interval '25 hours' WHERE tenant_id = $1`, w.tenant); err != nil {
		t.Fatal(err)
	}
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		n, err := q.DeleteExpiredIdempotency(ctx, w.tenant)
		if err == nil && n != 2 {
			t.Errorf("the lazy delete removed %d rows, want the grant's and the key's", n)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The Down and the Up again.
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		_, err := q.ClaimIdempotencyForGrant(ctx, store.ClaimIdempotencyForGrantParams{TenantID: w.tenant, GrantID: grant, IdemKey: "order-2", RequestHash: hash})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	up, down := opMigrationSections(t, "00106_key_the_idempotency_ledger_by_grant.sql")
	holdingLockedSavepoint(t, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00106 Down")
		var pk string
		if err := sp.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'public.api_idempotency'::regclass AND contype = 'p'`).Scan(&pk); err != nil ||
			pk != "PRIMARY KEY (tenant_id, key_id, idem_key)" {
			t.Errorf("after the Down the key is %q (%v), want 00052's", pk, err)
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_attribute WHERE attrelid = 'public.api_idempotency'::regclass AND attname = 'grant_id' AND NOT attisdropped`); n != 0 {
			t.Error("grant_id survives the Down")
		}
		opRunSection(t, ctx, sp, up, "00106 Up again")
		if got := opColumns(t, ctx, sp, "tappa_app", "api_idempotency", "INSERT"); got != "tenant_id,key_id,idem_key,request_hash,grant_id" {
			t.Errorf("tappa_app inserts %q after the Up, want the grant's column too", got)
		}
	})
}

// ------------------------------------------------------- the invitation gate --

// TestOAuthInviteGate_TheK22StatesAndTheLiveWritingGrant -- ADR 0047 §11.3-§11.4 through the shipped
// GetTenantAPIGate and ListLiveWritingOAuthClients:
//   - K-22's three states read from the schema: never opened (NULL, NULL), open (a time, a code),
//     closed by the operator (NULL, the code kept);
//   - the list names an app only for a LIVE grant carrying employees:write that still has a live
//     refresh token: a timesheets-only grant, a revoked grant and a grant whose refresh tokens are
//     all used or revoked (a dead grant -- the "Uygulama notları") are not listed; the list is ordered
//     by client id and has no LIMIT;
//   - step 3's composition: the first ENABLED app (resolve_oauth_client's disabled_at) opens the
//     gate -- with the first in order disabled and the second enabled, the gate is open (no miss).
func TestOAuthInviteGate_TheK22StatesAndTheLiveWritingGrant(t *testing.T) {
	ctx, tx := opTx(t)
	w := newOAuthWorld(t, ctx, tx)
	gate := func() store.GetTenantAPIGateRow {
		t.Helper()
		var g store.GetTenantAPIGateRow
		if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
			var err error
			g, err = q.GetTenantAPIGate(ctx, w.tenant)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return g
	}
	if g := gate(); g.ApiEnabledAt != nil || g.ApiCode != nil {
		t.Errorf("a business never opened reads %+v, want both NULL", g)
	}
	if _, err := tx.Exec(ctx, `UPDATE tenants SET api_code = 'OAGATE', api_enabled_at = clock_timestamp() WHERE id = $1`, w.tenant); err != nil {
		t.Fatal(err)
	}
	if g := gate(); g.ApiEnabledAt == nil || g.ApiCode == nil || *g.ApiCode != "OAGATE" {
		t.Errorf("an open business reads %+v", g)
	}
	if _, err := tx.Exec(ctx, `UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, w.tenant); err != nil {
		t.Fatal(err)
	}
	if g := gate(); g.ApiEnabledAt != nil || g.ApiCode == nil {
		t.Errorf("a business the operator closed reads %+v, want api_enabled_at NULL with the code kept", g)
	}

	// Four more apps, ordered by id below, so the list's order is known.
	apps := []uuid.UUID{w.client}
	for range 4 {
		apps = append(apps, oauthInsertClient(t, ctx, tx, w.operator.id))
	}
	slices.SortFunc(apps, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	writing := []string{"employees:write"}
	grantFor := func(client uuid.UUID, scopes []string) (grant, refresh uuid.UUID) {
		t.Helper()
		if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
			g, err := q.InsertOAuthGrant(ctx, store.InsertOAuthGrantParams{TenantID: w.tenant, ClientID: client, Scopes: scopes, CreatedBy: w.owner})
			if err != nil {
				return err
			}
			grant = g.ID
			r, err := q.InsertOAuthToken(ctx, store.InsertOAuthTokenParams{TenantID: w.tenant, GrantID: grant, Kind: "refresh", TokenHash: randHash(t)})
			refresh = r.ID
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return grant, refresh
	}
	grantFor(apps[0], writing)                                              // listed; its app will be disabled
	grantFor(apps[1], writing)                                              // listed; enabled -- the one that opens the gate
	grantFor(apps[2], []string{"timesheets:read"})                          // not listed: no employees:write
	dead, deadRefresh := grantFor(apps[3], writing)                         // not listed: its refresh token is used
	revokedGrant, _ := grantFor(apps[4], append(writing, "employees:read")) // not listed: revoked
	if _, err := tx.Exec(ctx, `UPDATE oauth_tokens SET used_at = clock_timestamp() WHERE id = $1`, deadRefresh); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_grants SET revoked_at = clock_timestamp(), revoked_reason = 'owner' WHERE id = $1`, revokedGrant); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`, apps[0]); err != nil {
		t.Fatal(err)
	}
	_ = dead
	var listed []uuid.UUID
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		var err error
		listed, err = q.ListLiveWritingOAuthClients(ctx, w.tenant)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(listed, apps[:2]) {
		t.Fatalf("ListLiveWritingOAuthClients = %v, want the two writing apps with a live refresh token, in id order: %v", listed, apps[:2])
	}
	// Step 3, as the queue will run it: resolve each in order, the first enabled one opens the gate.
	var opener uuid.UUID
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		for _, id := range listed {
			var disabled *string
			if err := sp.QueryRow(ctx, `SELECT disabled_at::text FROM resolve_oauth_client($1, NULL)`, id).Scan(&disabled); err != nil {
				return err
			}
			if disabled == nil {
				opener = id
				return nil
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if opener != apps[1] {
		t.Errorf("the gate's opener is %v, want the second app (the first is disabled): a disabled first app must not hide an enabled second", opener)
	}
	// Another tenant's context lists nothing of this business.
	other := newOAuthWorld(t, ctx, tx)
	if err := oauthAs(t, ctx, tx, other.tenant, func(q *store.Queries, _ pgx.Tx) error {
		l, err := q.ListLiveWritingOAuthClients(ctx, other.tenant)
		if err == nil && len(l) != 0 {
			t.Errorf("another business lists %v", l)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// TestOAuthQueries_ConnectedAppsTouchRevokeAndCleanup -- the remaining shipped statements of
// db/queries/oauth.sql: ListConnectedApps returns the business's live grants with the approver's name
// and status and has_live_refresh; TouchOAuthGrant writes last_used_at at most once a minute and never
// on a revoked grant; RevokeOAuthAccessToken ends one access token and not the grant (and never a
// refresh token); DisconnectOAuthGrant records the owner and the member; the two lazy deletes remove
// only what the restrictive policies allow -- an access token past its hour plus one, a code past a day.
func TestOAuthQueries_ConnectedAppsTouchRevokeAndCleanup(t *testing.T) {
	ctx, tx := opTx(t)
	w := newOAuthWorld(t, ctx, tx)
	grant, refresh, access := w.grant(t, ctx, tx, []string{"timesheets:read", "employees:read"})
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, _ pgx.Tx) error {
		apps, err := q.ListConnectedApps(ctx, w.tenant)
		if err != nil {
			return err
		}
		if len(apps) != 1 || apps[0].ID != grant || apps[0].ClientID != w.client || apps[0].ApproverName != "API-3 Owner" ||
			apps[0].ApproverStatus != "active" || !apps[0].HasLiveRefresh || apps[0].LastUsedAt != nil {
			t.Errorf("ListConnectedApps = %+v", apps)
		}
		for i, want := range []int64{1, 0} {
			if n, err := q.TouchOAuthGrant(ctx, store.TouchOAuthGrantParams{TenantID: w.tenant, ID: grant}); err != nil || n != want {
				t.Errorf("touch #%d: %d (%v), want %d -- at most once a minute", i+1, n, err, want)
			}
		}
		if n, err := q.RevokeOAuthAccessToken(ctx, store.RevokeOAuthAccessTokenParams{TenantID: w.tenant, TokenID: refresh}); err != nil || n != 0 {
			t.Errorf("RevokeOAuthAccessToken on a refresh token: %d (%v), want 0", n, err)
		}
		if n, err := q.RevokeOAuthAccessToken(ctx, store.RevokeOAuthAccessTokenParams{TenantID: w.tenant, TokenID: access}); err != nil || n != 1 {
			t.Errorf("RevokeOAuthAccessToken: %d (%v), want 1", n, err)
		}
		if _, err := q.LockLiveOAuthGrant(ctx, store.LockLiveOAuthGrantParams{TenantID: w.tenant, GrantID: grant}); err != nil {
			t.Errorf("revoking an access token ended the grant: %v", err)
		}
		if n, err := q.DisconnectOAuthGrant(ctx, store.DisconnectOAuthGrantParams{Actor: w.owner, ID: grant, TenantID: w.tenant}); err != nil || n != 1 {
			t.Errorf("DisconnectOAuthGrant: %d (%v), want 1", n, err)
		}
		if n, err := q.TouchOAuthGrant(ctx, store.TouchOAuthGrantParams{TenantID: w.tenant, ID: grant}); err != nil || n != 0 {
			t.Errorf("touching a revoked grant: %d (%v), want 0", n, err)
		}
		if apps, err := q.ListConnectedApps(ctx, w.tenant); err != nil || len(apps) != 0 {
			t.Errorf("a disconnected grant is still listed: %v (%v)", apps, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var reason string
	var by uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT revoked_reason, revoked_by FROM oauth_grants WHERE id = $1`, grant).Scan(&reason, &by); err != nil || reason != "owner" || by != w.owner {
		t.Errorf("the disconnection reads %q by %v (%v), want owner by the member", reason, by, err)
	}
	// The lazy deletes: rows the owner writes as old as each bound needs, and one just inside it.
	g2, _, _ := w.grant(t, ctx, tx, []string{"timesheets:read"})
	for _, ago := range []string{"2 hours 1 second", "1 hour 59 minutes"} { // expired 1 h 0 m 1 s and 59 m ago
		if _, err := tx.Exec(ctx, `INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, scopes, created_at, expires_at)
		                           VALUES ($1, $2, 'access', $3, '{timesheets:read}', now() - $4::interval, now() - $4::interval + interval '1 hour')`,
			w.tenant, g2, randHash(t), ago); err != nil {
			t.Fatal(err)
		}
	}
	for _, ago := range []string{"25 hours", "23 hours"} {
		if _, err := tx.Exec(ctx, `INSERT INTO oauth_codes (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes, code_challenge, created_at, expires_at)
		                           VALUES ($1, $2, $3, $4, $5, '{timesheets:read}', $6, now() - $7::interval, now() - $7::interval + interval '60 seconds')`,
			w.tenant, randHash(t), w.client, w.owner, oauthGoodRedirect, strings.Repeat("g", 43), ago); err != nil {
			t.Fatal(err)
		}
	}
	if err := oauthAs(t, ctx, tx, w.tenant, func(q *store.Queries, sp pgx.Tx) error {
		if n, err := q.DeleteExpiredOAuthTokensForGrant(ctx, store.DeleteExpiredOAuthTokensForGrantParams{TenantID: w.tenant, GrantID: g2}); err != nil || n != 1 {
			t.Errorf("DeleteExpiredOAuthTokensForGrant: %d (%v), want 1 -- the access token past its hour plus one", n, err)
		}
		if n, err := q.DeleteExpiredOAuthCodes(ctx, w.tenant); err != nil || n != 1 {
			t.Errorf("DeleteExpiredOAuthCodes: %d (%v), want 1 -- the code past a day", n, err)
		}
		// The restrictive policy is the database's half: a bare DELETE of a live token removes nothing.
		var n int64
		if err := sp.QueryRow(ctx, `WITH d AS (DELETE FROM oauth_tokens WHERE grant_id = $1 RETURNING 1) SELECT count(*) FROM d`, g2).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			t.Errorf("a bare DELETE removed %d live tokens; the restrictive policy must refuse them", n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
