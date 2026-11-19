package db

// liveqrdisplays_test.go -- migration 00111 (M16-19, ADR 0045 §2.5): live_qr_displays and the three
// live QR screen resolvers, against real Postgres. Nothing here prints a code, a cookie or a digest of
// either: values are compared, never shown (§7).

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

const op00111File = "00111_create_live_qr_displays.sql"

// liveQRMigration is 00111's number.
const liveQRMigration = 111

// liveQRSetAsideSQL removes every screen inside a walk's transaction so 00111's Down -- which refuses
// while the table holds a row -- can run; the rollback brings them back. No trigger guards a delete on
// the table (tappa_app holds no DELETE), so the owner needs nothing switched off.
const liveQRSetAsideSQL = `DELETE FROM public.live_qr_displays;`

// liveQRResolvers are the three functions 00111 adds, with their exact arguments and results.
var liveQRResolvers = []struct {
	sig, args, result string
}{
	{"resolve_live_qr_display_by_pairing_code_hash(text)", "p_code_hash text",
		"TABLE(id uuid, tenant_id uuid, location_id uuid, pairing_expires_at timestamp with time zone, revoked_at timestamp with time zone)"},
	{"resolve_live_qr_display_by_session_hash(text)", "p_session_hash text",
		"TABLE(id uuid, tenant_id uuid, location_id uuid, revoked_at timestamp with time zone)"},
	{"resolve_live_qr_display_by_id(uuid)", "p_display_id uuid",
		"TABLE(id uuid, tenant_id uuid, location_id uuid, revoked_at timestamp with time zone, location_active boolean)"},
}

// addScreen creates a screen waiting for its code at w's venue through the product's query, as
// tappa_app in w's context, and returns its id and the code's digest.
func (w appDeviceWorld) addScreen(t *testing.T, ctx context.Context, tx pgx.Tx) (uuid.UUID, string) {
	t.Helper()
	code := opRandHex(t)
	var id uuid.UUID
	if err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		row, err := store.New(sp).CreateLiveQRDisplay(ctx, store.CreateLiveQRDisplayParams{
			TenantID: w.tenant, LocationID: w.venue, PairingCodeHash: code, TtlSeconds: 600,
		})
		id = row.ID
		return err
	}); err != nil {
		t.Fatalf("create a live QR screen: %v", err)
	}
	return id, code
}

// pairScreen consumes the screen's code with a fresh session digest, as tappa_app, and returns the
// session digest and the statement's error.
func (w appDeviceWorld) pairScreen(t *testing.T, ctx context.Context, tx pgx.Tx, id uuid.UUID, code string) (string, error) {
	t.Helper()
	session := opRandHex(t)
	err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).ConsumeLiveQRDisplayPairing(ctx, store.ConsumeLiveQRDisplayPairingParams{
			SessionHash: session, TenantID: w.tenant, ID: id, PairingCodeHash: code,
		})
		return err
	})
	return session, err
}

// TestLiveQRDisplaysCatalog measures, in the live catalog, ADR 0002 md.7's five constraints on each
// of the three resolvers and the RLS five on the table:
//
//	definer   owner tappa_resolver (NOSUPERUSER, BYPASSRLS), SECURITY DEFINER, proconfig exactly
//	          search_path = pg_catalog, pg_temp, STABLE, one function of that name (no overload)
//	(i)       the argument list is exactly the key
//	(ii)      at most one row: the two digests are UNIQUE, id is the primary key
//	(iii)     the result is exactly the fixed column list
//	(iv)      EXECUTE: tappa_app, PUBLIC and the other roles not; tappa_resolver reads exactly the
//	          seven columns of live_qr_displays and id, tenant_id, active of locations
//	(v)       the table's one policy is the standard NULLIF form with no OR branch
//	RLS five  ENABLE + FORCE, an index led by tenant_id, tenant_id NOT NULL
//	grants    tappa_app's verbs and columns (ADR 0028 §1's `arwd` rule)
//	trigger   the one-way function refuses another table (TG_RELID); EXECUTE in its owner alone
func TestLiveQRDisplaysCatalog(t *testing.T) {
	ctx, tx := opTx(t)
	for _, r := range liveQRResolvers {
		var (
			owner, cfg, args, result, body, volatile string
			super, bypass, secdef                    bool
		)
		if err := tx.QueryRow(ctx, `
			SELECT pg_get_userbyid(p.proowner), r.rolsuper, r.rolbypassrls, p.prosecdef, array_to_string(p.proconfig, '|'),
			       pg_get_function_arguments(p.oid), pg_get_function_result(p.oid), p.prosrc, p.provolatile::text
			  FROM pg_proc p JOIN pg_roles r ON r.oid = p.proowner
			 WHERE p.oid = ('public.' || $1)::regprocedure`, r.sig).
			Scan(&owner, &super, &bypass, &secdef, &cfg, &args, &result, &body, &volatile); err != nil {
			t.Fatalf("read %s: %v", r.sig, err)
		}
		if owner != "tappa_resolver" || super || !bypass || !secdef {
			t.Errorf("%s: owner %s super %v bypassrls %v secdef %v; want tappa_resolver, false, true, true", r.sig, owner, super, bypass, secdef)
		}
		if cfg != "search_path=pg_catalog, pg_temp" || volatile != "s" {
			t.Errorf("%s: proconfig %q, volatility %q; want the fixed search_path and STABLE", r.sig, cfg, volatile)
		}
		name, _, _ := strings.Cut(r.sig, "(")
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname = $1`, name); n != 1 {
			t.Errorf("%d functions named %s; an overload is a second door", n, name)
		}
		if args != r.args {
			t.Errorf("(i) %s arguments %q, want %q", r.sig, args, r.args)
		}
		if result != r.result {
			t.Errorf("(iii) %s result %q, want %q", r.sig, result, r.result)
		}
		if !strings.Contains(body, "public.live_qr_displays") || strings.Contains(strings.ToUpper(body), "EXECUTE") ||
			strings.Contains(body, "format(") || strings.Contains(body, "*") {
			t.Errorf("%s body: want qualified tables only, no SELECT * and no dynamic SQL:\n%s", r.sig, body)
		}
		if strings.Contains(body, "locations") != strings.HasPrefix(r.sig, "resolve_live_qr_display_by_id(") {
			t.Errorf("%s body: only the tap's resolver joins locations:\n%s", r.sig, body)
		}
		for _, c := range []struct {
			who  string
			want bool
		}{{"public", false}, {"tappa_app", true}, {"tappa_operator", false}, {"tappa_opdefiner", false},
			{"tappa_holdingdefiner", false}, {"tappa_feedwriter", false}, {"tappa_identitydefiner", false}} {
			var may bool
			if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1, ('public.' || $2)::regprocedure, 'EXECUTE')`, c.who, r.sig).Scan(&may); err != nil {
				t.Fatal(err)
			}
			if may != c.want {
				t.Errorf("(iv) %s EXECUTE on %s = %v, want %v", c.who, r.sig, may, c.want)
			}
		}
	}
	// (ii) the keys the <= 1 row rests on.
	for _, c := range []struct{ name, def string }{
		{"live_qr_displays_pkey", "PRIMARY KEY (id)"},
		{"live_qr_displays_pairing_code_hash_key", "UNIQUE (pairing_code_hash)"},
		{"live_qr_displays_session_hash_key", "UNIQUE (session_hash)"},
		{"live_qr_displays_location_fk", "FOREIGN KEY (location_id, tenant_id) REFERENCES locations(id, tenant_id) ON DELETE RESTRICT"},
	} {
		var def string
		if err := tx.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
		                             WHERE conrelid = 'public.live_qr_displays'::regclass AND conname = $1`, c.name).Scan(&def); err != nil {
			t.Fatalf("(ii) %s: %v", c.name, err)
		}
		if def != c.def {
			t.Errorf("(ii) %s is %q, want %q", c.name, def, c.def)
		}
	}

	var policy string
	if err := tx.QueryRow(ctx, `SELECT pg_get_expr(polqual, polrelid) || ' / ' || pg_get_expr(polwithcheck, polrelid)
	                              FROM pg_policy WHERE polrelid = 'public.live_qr_displays'::regclass`).Scan(&policy); err != nil {
		t.Fatalf("(v) the one policy: %v", err)
	}
	if strings.Count(policy, "NULLIF") != 2 || strings.Contains(strings.ToUpper(policy), " OR ") {
		t.Errorf("(v) policy %q, want the NULLIF form in USING and WITH CHECK and no OR branch", policy)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_policy WHERE polrelid = 'public.live_qr_displays'::regclass`); n != 1 {
		t.Errorf("live_qr_displays holds %d policies, want 1 (a second permissive policy ORs into the first)", n)
	}
	var enabled, forced, notNull bool
	if err := tx.QueryRow(ctx, `SELECT c.relrowsecurity, c.relforcerowsecurity, a.attnotnull
	                              FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id'
	                             WHERE c.oid = 'public.live_qr_displays'::regclass`).Scan(&enabled, &forced, &notNull); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced || !notNull {
		t.Errorf("live_qr_displays: RLS ENABLE %v FORCE %v, tenant_id NOT NULL %v", enabled, forced, notNull)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
	                            WHERE i.indrelid = 'public.live_qr_displays'::regclass AND a.attname = 'tenant_id'`); n < 1 {
		t.Error("live_qr_displays: no index leads with tenant_id")
	}

	// tappa_app's reach, column by column (ADR 0028 §1).
	for _, c := range []struct{ priv, want string }{
		{"SELECT", "id,tenant_id,location_id,pairing_code_hash,pairing_expires_at,paired_at,created_at,revoked_at"},
		{"INSERT", "tenant_id,location_id,pairing_code_hash,pairing_expires_at"},
		{"UPDATE", "pairing_code_hash,paired_at,session_hash,revoked_at"},
		{"REFERENCES", ""},
	} {
		if got := opColumns(t, ctx, tx, "tappa_app", "live_qr_displays", c.priv); got != c.want {
			t.Errorf("tappa_app %s on live_qr_displays: %q, want %q", c.priv, got, c.want)
		}
	}
	for _, priv := range []string{"DELETE", "TRUNCATE", "TRIGGER"} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_table_privilege('tappa_app', 'public.live_qr_displays', $1)`, priv).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may {
			t.Errorf("tappa_app holds %s on live_qr_displays; a screen is revoked, never removed", priv)
		}
	}
	// tappa_resolver: seven columns of the table and three of locations, SELECT only.
	if got := opColumns(t, ctx, tx, "tappa_resolver", "live_qr_displays", "SELECT"); got != "id,tenant_id,location_id,pairing_code_hash,pairing_expires_at,session_hash,revoked_at" {
		t.Errorf("tappa_resolver SELECT on live_qr_displays: %q, want the resolutions' seven", got)
	}
	if got := opColumns(t, ctx, tx, "tappa_resolver", "locations", "SELECT"); got != "id,tenant_id,active" {
		t.Errorf("tappa_resolver SELECT on locations: %q, want id,tenant_id,active (never the evidence columns)", got)
	}
	for _, table := range []string{"live_qr_displays", "locations"} {
		for _, priv := range []string{"INSERT", "UPDATE", "REFERENCES"} {
			if got := opColumns(t, ctx, tx, "tappa_resolver", table, priv); got != "" {
				t.Errorf("tappa_resolver %s on %s: %q, want none", priv, table, got)
			}
		}
	}

	// Live: each resolver one row for its key, none for an unknown one, as a context-less tappa_app;
	// and without the functions a context-less tappa_app sees no screen and cannot read a session.
	w := newAppDeviceWorld(t, ctx, tx)
	waiting, code := w.addScreen(t, ctx, tx)
	paired, pairedCode := w.addScreen(t, ctx, tx)
	session, err := w.pairScreen(t, ctx, tx, paired, pairedCode)
	if err != nil {
		t.Fatalf("pair a screen: %v", err)
	}
	if err := w.asApp(t, ctx, tx, uuid.Nil, func(sp pgx.Tx) error {
		for _, c := range []struct {
			sql      string
			key, bad any
		}{
			{`SELECT count(*) FROM resolve_live_qr_display_by_pairing_code_hash($1)`, code, opRandHex(t)},
			{`SELECT count(*) FROM resolve_live_qr_display_by_session_hash($1)`, session, opRandHex(t)},
			{`SELECT count(*) FROM resolve_live_qr_display_by_id($1)`, waiting, uuid.New()},
		} {
			if n := opInt(t, ctx, sp, c.sql, c.key); n != 1 {
				t.Errorf("%s with its key: %d rows, want 1", c.sql, n)
			}
			if n := opInt(t, ctx, sp, c.sql, c.bad); n != 0 {
				t.Errorf("%s with an unknown key: %d rows, want 0", c.sql, n)
			}
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM live_qr_displays WHERE id = ANY($1)`, []uuid.UUID{waiting, paired}); n != 0 {
			t.Errorf("a context-less tappa_app sees %d screens, want 0", n)
		}
		return nil
	}); err != nil {
		t.Fatalf("resolve as tappa_app: %v", err)
	}
	opWant(t, w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `SELECT session_hash FROM live_qr_displays WHERE id = $1`, paired)
		return err
	}), sqlstateInsufficientPrivi, "tappa_app reading session_hash directly")

	// The trigger function: EXECUTE in its owner alone, bound BEFORE INSERT (born waiting) and BEFORE
	// UPDATE (the history); bound elsewhere it refuses.
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname = 'live_qr_displays_state_is_one_way'
	                            AND NOT prosecdef AND proacl::text = '{tappa_owner=X/tappa_owner}'`); n != 1 {
		t.Error("live_qr_displays_state_is_one_way is not an INVOKER function with EXECUTE in its owner alone")
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_trigger WHERE tgrelid = 'public.live_qr_displays'::regclass AND NOT tgisinternal
	                            AND tgfoid = 'public.live_qr_displays_state_is_one_way()'::regprocedure
	                            AND tgname IN ('live_qr_displays_born_waiting', 'live_qr_displays_state_one_way')`); n != 2 {
		t.Errorf("the state function is bound by %d of its two triggers (born waiting, state one way)", n)
	}
	for _, s := range []string{
		`CREATE TEMP TABLE zz_m1619_bound (revoked_at timestamptz)`,
		`CREATE TRIGGER zz_m1619 BEFORE UPDATE ON zz_m1619_bound FOR EACH ROW EXECUTE FUNCTION public.live_qr_displays_state_is_one_way()`,
		`INSERT INTO zz_m1619_bound VALUES (now())`,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	opWant(t, opTry(t, ctx, tx, `UPDATE zz_m1619_bound SET revoked_at = NULL`), sqlstatePrerequisiteState,
		"the screen trigger function on a table that is not live_qr_displays")
}

// TestRLS_LiveQRDisplays_TenantIsolation is CLAUDE.md §8's RLS test for the new table, in the
// FILTERLESS shape (§6): as tappa_app in B's context a bare `WHERE id = <A's screen>` is 0 rows and
// the positive control in A's context is 1 -- so the 0 is RLS's, not a WHERE's. B cannot write a
// screen stamped with A's tenant (WITH CHECK, 42501), cannot stand a screen of its own at A's location
// (the composite key, 23503), cannot revoke A's screen nor consume A's code (0 rows), and a
// context-less tappa_app sees nothing (NULLIF). The only way across is the resolvers
// (TestResolveLiveQRDisplay_ReadsAcrossTenantsAndCarriesTheState).
//
// CONTROL: with RLS off on the table B's bare read sees A's screen -- the probe reaches the row.
func TestRLS_LiveQRDisplays_TenantIsolation(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	screenA, codeA := a.addScreen(t, ctx, tx)
	count := func(ctxTenant uuid.UUID, sql string, args ...any) int64 {
		var n int64
		if err := a.asApp(t, ctx, tx, ctxTenant, func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, sql, args...).Scan(&n)
		}); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	for _, sql := range []string{
		`SELECT count(*) FROM live_qr_displays WHERE id = $1`,
		`SELECT count(*) FROM live_qr_displays WHERE pairing_code_hash = (SELECT pairing_code_hash FROM live_qr_displays WHERE id = $1)`,
	} {
		if n := count(b.tenant, sql, screenA); n != 0 {
			t.Fatalf("B's context: %q = %d, want 0", sql, n)
		}
		if n := count(a.tenant, sql, screenA); n != 1 {
			t.Fatalf("POSITIVE CONTROL: A's context: %q = %d, want 1", sql, n)
		}
		if n := count(uuid.Nil, sql, screenA); n != 0 {
			t.Fatalf("no context: %q = %d, want 0", sql, n)
		}
	}
	if n := count(b.tenant, `WITH u AS (UPDATE live_qr_displays SET revoked_at = now() WHERE id = $1 RETURNING 1) SELECT count(*) FROM u`, screenA); n != 0 {
		t.Fatalf("B revoked %d of A's screens, want 0", n)
	}
	if n := count(b.tenant, `WITH u AS (UPDATE live_qr_displays SET pairing_code_hash = NULL, paired_at = now(), session_hash = $2
	                                    WHERE pairing_code_hash = $1 RETURNING 1) SELECT count(*) FROM u`, codeA, opRandHex(t)); n != 0 {
		t.Fatalf("B paired %d of A's screens, want 0", n)
	}
	if err := b.asApp(t, ctx, tx, b.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).ConsumeLiveQRDisplayPairing(ctx, store.ConsumeLiveQRDisplayPairingParams{
			SessionHash: opRandHex(t), TenantID: a.tenant, ID: screenA, PairingCodeHash: codeA,
		})
		return err
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("B consuming A's code through the product's query, naming A's tenant: %v, want pgx.ErrNoRows", err)
	}
	opWant(t, b.asApp(t, ctx, tx, b.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `INSERT INTO live_qr_displays (tenant_id, location_id, pairing_code_hash, pairing_expires_at)
		                         VALUES ($1, $2, $3, now() + interval '5 minutes')`, a.tenant, a.venue, opRandHex(t))
		return err
	}), sqlstateInsufficientPrivi, "B inserting a screen stamped with A's tenant")
	opWant(t, b.asApp(t, ctx, tx, b.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `INSERT INTO live_qr_displays (tenant_id, location_id, pairing_code_hash, pairing_expires_at)
		                         VALUES ($1, $2, $3, now() + interval '5 minutes')`, b.tenant, a.venue, opRandHex(t))
		return err
	}), sqlstateForeignKeyViolation, "B standing a screen of its own at A's location")
	if n := count(a.tenant, `SELECT count(*) FROM live_qr_displays WHERE id = $1 AND revoked_at IS NULL AND paired_at IS NULL
	                          AND pairing_code_hash = $2`, screenA, codeA); n != 1 {
		t.Fatal("A's screen was touched by B's attempts")
	}

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`ALTER TABLE live_qr_displays NO FORCE ROW LEVEL SECURITY`, `ALTER TABLE live_qr_displays DISABLE ROW LEVEL SECURITY`} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	var n int64
	if err := opAs(t, ctx, sp, "tappa_app", func(as pgx.Tx) error {
		if _, err := as.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, b.tenant.String()); err != nil {
			return err
		}
		return as.QueryRow(ctx, `SELECT count(*) FROM live_qr_displays WHERE id = $1`, screenA).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("CONTROL: with RLS off B's bare read sees %d of A's screens, want 1 -- the probe is not reaching the row", n)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestResolveLiveQRDisplay_ReadsAcrossTenantsAndCarriesTheState: the tap's resolver answers A's screen
// in B's context (the holding case, ADR 0027 -- the session's tenant is not the screen's) and with no
// context, with A's tenant and location; every resolver CARRIES the state instead of hiding it -- a
// revoked screen, an inactive location and an expired code each resolve to their one row with the
// state set (a resolver that filtered would answer "unknown" and the caller could not tell §5 row 1's
// twin from noise). A consumed code resolves nothing; the session it left resolves. The scans are the
// Go wrappers' SQL and column order (resolve.go's constants).
func TestResolveLiveQRDisplay_ReadsAcrossTenantsAndCarriesTheState(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	screen, code := a.addScreen(t, ctx, tx)
	byID := func(ctxTenant uuid.UUID) []ResolvedLiveQRDisplay {
		var out []ResolvedLiveQRDisplay
		if err := a.asApp(t, ctx, tx, ctxTenant, func(sp pgx.Tx) error {
			rows, err := sp.Query(ctx, getLiveQRDisplayByIDSQL, screen)
			if err != nil {
				return err
			}
			out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ResolvedLiveQRDisplay, error) {
				var d ResolvedLiveQRDisplay
				err := r.Scan(&d.ID, &d.TenantID, &d.LocationID, &d.RevokedAt, &d.LocationActive)
				return d, err
			})
			return err
		}); err != nil {
			t.Fatalf("resolve by id: %v", err)
		}
		return out
	}
	for _, who := range []uuid.UUID{b.tenant, uuid.Nil, a.tenant} {
		got := byID(who)
		if len(got) != 1 || got[0].ID != screen || got[0].TenantID != a.tenant || got[0].LocationID != a.venue ||
			got[0].RevokedAt != nil || !got[0].LocationActive {
			t.Fatalf("context %v: the tap's resolver answered %+v, want A's live screen at A's active venue", who, got)
		}
	}

	pairing := func() []ResolvedLiveQRPairing {
		var out []ResolvedLiveQRPairing
		if err := b.asApp(t, ctx, tx, uuid.Nil, func(sp pgx.Tx) error {
			rows, err := sp.Query(ctx, getLiveQRDisplayByPairingCodeHashSQL, code)
			if err != nil {
				return err
			}
			out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ResolvedLiveQRPairing, error) {
				var p ResolvedLiveQRPairing
				err := r.Scan(&p.ID, &p.TenantID, &p.LocationID, &p.PairingExpiresAt, &p.RevokedAt)
				return p, err
			})
			return err
		}); err != nil {
			t.Fatalf("resolve the pairing: %v", err)
		}
		return out
	}
	if got := pairing(); len(got) != 1 || got[0].ID != screen || got[0].TenantID != a.tenant || got[0].LocationID != a.venue ||
		got[0].RevokedAt != nil || got[0].PairingExpiresAt.IsZero() {
		t.Fatalf("the pairing resolver answered %+v, want A's waiting screen", got)
	}
	// An expired code is carried: the owner moves the screen's clock back (tappa_app cannot; the
	// one-way trigger does not watch these two columns).
	if _, err := tx.Exec(ctx, `UPDATE live_qr_displays SET created_at = now() - interval '2 hours', pairing_expires_at = now() - interval '90 minutes' WHERE id = $1`, screen); err != nil {
		t.Fatal(err)
	}
	if got := pairing(); len(got) != 1 {
		t.Fatalf("an expired code resolves to %d rows, want 1 (the expiry is carried, not filtered)", len(got))
	}
	if _, err := tx.Exec(ctx, `UPDATE live_qr_displays SET created_at = now(), pairing_expires_at = now() + interval '10 minutes' WHERE id = $1`, screen); err != nil {
		t.Fatal(err)
	}

	session, err := a.pairScreen(t, ctx, tx, screen, code)
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	if got := pairing(); len(got) != 0 {
		t.Fatalf("a consumed code resolves to %d rows, want 0", len(got))
	}
	bySession := func() []ResolvedLiveQRSession {
		var out []ResolvedLiveQRSession
		if err := b.asApp(t, ctx, tx, uuid.Nil, func(sp pgx.Tx) error {
			rows, err := sp.Query(ctx, getLiveQRDisplayBySessionHashSQL, session)
			if err != nil {
				return err
			}
			out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ResolvedLiveQRSession, error) {
				var s ResolvedLiveQRSession
				err := r.Scan(&s.ID, &s.TenantID, &s.LocationID, &s.RevokedAt)
				return s, err
			})
			return err
		}); err != nil {
			t.Fatalf("resolve the session: %v", err)
		}
		return out
	}
	if got := bySession(); len(got) != 1 || got[0].ID != screen || got[0].TenantID != a.tenant || got[0].RevokedAt != nil {
		t.Fatalf("the session resolver answered %+v, want A's paired screen", got)
	}

	if err := a.asApp(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).RevokeLiveQRDisplay(ctx, store.RevokeLiveQRDisplayParams{TenantID: a.tenant, ID: screen})
		return err
	}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE locations SET active = false WHERE id = $1`, a.venue); err != nil {
		t.Fatal(err)
	}
	if got := bySession(); len(got) != 1 || got[0].RevokedAt == nil {
		t.Fatalf("a revoked screen's session resolves to %+v, want its one row with the stamp", got)
	}
	if got := byID(b.tenant); len(got) != 1 || got[0].RevokedAt == nil || got[0].LocationActive {
		t.Fatalf("a revoked screen at an inactive venue resolves to %+v, want its one row carrying both", got)
	}
}

// TestLiveQRDisplays_PairingIsSingleUseAndOneWay drives the product's queries through every answer
// the screen page (M16-20) has to tell apart from success: a waiting screen pairs once; again, it is
// refused; a wrong code, another screen's code, an expired code and a revoked screen are refused; a
// pairing lifetime over an hour and a digest that is not 64 lower-case hex are refused; tappa_app
// cannot write created_at or a state column at INSERT. The trigger holds the history, for tappa_app
// and for the owner: a paired screen keeps its session, an unpaired screen's code is not swapped, a
// revoked screen is final, both stamps are the moment they happen. A second revoke is idempotent.
func TestLiveQRDisplays_PairingIsSingleUseAndOneWay(t *testing.T) {
	ctx, tx := opTx(t)
	w := newAppDeviceWorld(t, ctx, tx)
	screen, code := w.addScreen(t, ctx, tx)
	other, otherCode := w.addScreen(t, ctx, tx)
	exec := func(sql string, args ...any) error {
		return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, sql, args...)
			return err
		})
	}
	if _, err := w.pairScreen(t, ctx, tx, screen, opRandHex(t)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a wrong code: %v, want pgx.ErrNoRows", err)
	}
	if _, err := w.pairScreen(t, ctx, tx, screen, otherCode); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another screen's code: %v, want pgx.ErrNoRows", err)
	}
	session, err := w.pairScreen(t, ctx, tx, screen, code)
	if err != nil {
		t.Fatalf("POSITIVE CONTROL: pairing a waiting screen: %v", err)
	}
	if _, err := w.pairScreen(t, ctx, tx, screen, code); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("pairing it again with its code: %v, want pgx.ErrNoRows (single use)", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM live_qr_displays WHERE id = $1 AND pairing_code_hash IS NULL AND session_hash = $2 AND paired_at IS NOT NULL`,
		screen, session); n != 1 {
		t.Fatal("the pairing did not clear the code and store the session")
	}

	// An expired code (written by the owner with a past lifetime) and a revoked screen.
	expired, expiredCode := uuid.New(), opRandHex(t)
	if _, err := tx.Exec(ctx, `INSERT INTO live_qr_displays (id, tenant_id, location_id, pairing_code_hash, created_at, pairing_expires_at)
	                           VALUES ($1, $2, $3, $4, now() - interval '2 hours', now() - interval '90 minutes')`, expired, w.tenant, w.venue, expiredCode); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pairScreen(t, ctx, tx, expired, expiredCode); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an expired code: %v, want pgx.ErrNoRows", err)
	}
	if err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).RevokeLiveQRDisplay(ctx, store.RevokeLiveQRDisplayParams{TenantID: w.tenant, ID: other})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.pairScreen(t, ctx, tx, other, otherCode); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a revoked screen's code: %v, want pgx.ErrNoRows", err)
	}

	// The shape.
	opWant(t, w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).CreateLiveQRDisplay(ctx, store.CreateLiveQRDisplayParams{TenantID: w.tenant, LocationID: w.venue, PairingCodeHash: opRandHex(t), TtlSeconds: 3601})
		return err
	}), sqlstateCheckViolation, "a pairing code living longer than an hour")
	opWant(t, w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).CreateLiveQRDisplay(ctx, store.CreateLiveQRDisplayParams{TenantID: w.tenant, LocationID: w.venue, PairingCodeHash: "NOT-HEX", TtlSeconds: 60})
		return err
	}), sqlstateCheckViolation, "a code digest that is not 64 lower-case hex")
	opWant(t, exec(`INSERT INTO live_qr_displays (tenant_id, location_id, pairing_code_hash, pairing_expires_at, created_at)
	                VALUES ($1, $2, $3, now() + interval '5 minutes', now() - interval '1 hour')`, w.tenant, w.venue, opRandHex(t)),
		sqlstateInsufficientPrivi, "tappa_app writing created_at")
	opWant(t, exec(`INSERT INTO live_qr_displays (tenant_id, location_id, session_hash, paired_at, pairing_expires_at)
	                VALUES ($1, $2, $3, now(), now() + interval '5 minutes')`, w.tenant, w.venue, opRandHex(t)),
		sqlstateInsufficientPrivi, "tappa_app inserting a screen already paired")
	opWant(t, exec(`UPDATE live_qr_displays SET pairing_expires_at = pairing_expires_at + interval '1 hour' WHERE id = $1`, screen),
		sqlstateInsufficientPrivi, "tappa_app extending a pairing")
	opWant(t, exec(`UPDATE live_qr_displays SET location_id = location_id WHERE id = $1`, screen),
		sqlstateInsufficientPrivi, "tappa_app moving a screen")
	opWant(t, exec(`DELETE FROM live_qr_displays WHERE id = $1`, screen), sqlstateInsufficientPrivi, "tappa_app deleting a screen")

	// The history, for tappa_app and for the owner.
	waiting, _ := w.addScreen(t, ctx, tx)
	for _, c := range []struct{ what, sql string }{
		{"a paired screen given another session", `UPDATE live_qr_displays SET session_hash = '` + opRandHex(t) + `' WHERE id = $1`},
		{"a paired screen re-stamped", `UPDATE live_qr_displays SET paired_at = paired_at + interval '1 second' WHERE id = $1`},
		{"a paired screen given a code again", `UPDATE live_qr_displays SET pairing_code_hash = '` + opRandHex(t) + `', paired_at = NULL, session_hash = NULL WHERE id = $1`},
	} {
		opWant(t, exec(c.sql, screen), sqlstateRestrictViolation, "tappa_app: "+c.what)
		opWant(t, opTry(t, ctx, tx, c.sql, screen), sqlstateRestrictViolation, "the owner: "+c.what)
	}
	opWant(t, exec(`UPDATE live_qr_displays SET pairing_code_hash = $2 WHERE id = $1`, waiting, opRandHex(t)),
		sqlstateRestrictViolation, "an unpaired screen's code swapped")
	opWant(t, exec(`UPDATE live_qr_displays SET pairing_code_hash = NULL, paired_at = now() - interval '1 second', session_hash = $2 WHERE id = $1`, waiting, opRandHex(t)),
		sqlstateRestrictViolation, "a back-dated pairing")
	opWant(t, exec(`UPDATE live_qr_displays SET revoked_at = now() - interval '1 second' WHERE id = $1`, waiting),
		sqlstateRestrictViolation, "a back-dated revocation")
	var first, second *string
	revoke := func(dst **string) {
		if err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
			row, err := store.New(sp).RevokeLiveQRDisplay(ctx, store.RevokeLiveQRDisplayParams{TenantID: w.tenant, ID: screen})
			if err == nil && row.RevokedAt != nil {
				s := row.RevokedAt.String()
				*dst = &s
			}
			return err
		}); err != nil {
			t.Fatalf("revoke: %v", err)
		}
	}
	revoke(&first)
	revoke(&second)
	if first == nil || second == nil || *first != *second {
		t.Fatalf("a second revoke moved the stamp (%v -> %v); COALESCE keeps the first", first, second)
	}
	opWant(t, exec(`UPDATE live_qr_displays SET revoked_at = NULL WHERE id = $1`, screen), sqlstateRestrictViolation, "tappa_app un-revoking a screen")
	opWant(t, opTry(t, ctx, tx, `UPDATE live_qr_displays SET revoked_at = NULL WHERE id = $1`, screen), sqlstateRestrictViolation, "the owner un-revoking a screen")
	if err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := store.New(sp).RevokeLiveQRDisplay(ctx, store.RevokeLiveQRDisplayParams{TenantID: w.tenant, ID: uuid.New()})
		return err
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("revoking an unknown screen: %v, want pgx.ErrNoRows", err)
	}

	var list []store.ListLiveQRDisplaysForLocationRow
	if err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		var err error
		list, err = store.New(sp).ListLiveQRDisplaysForLocation(ctx, store.ListLiveQRDisplaysForLocationParams{TenantID: w.tenant, LocationID: w.venue})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("the location lists %d screens, want 4 (paired+revoked, revoked, expired, waiting)", len(list))
	}
}

// TestLiveQRDisplays_ConcurrentPairingExactlyOnce is CLAUDE.md §8's replay test for the pairing
// code: N goroutines, each in its own WithTenant transaction on its own pooled connection, consume
// the SAME code at once with different session digests -- exactly one RETURNING row, every other one
// pgx.ErrNoRows, and the row holds the winner's session. Run with -race. The rows are committed
// (they must be visible to other connections) and stay inert, the rls_test.go fixture convention;
// liveQRSetAsideSQL takes them out of a walk's way.
func TestLiveQRDisplays_ConcurrentPairingExactlyOnce(t *testing.T) {
	d := appDB(t)
	tenantID, venue := m11Tenant(t, d)
	code := opRandHex(t)
	var screen uuid.UUID
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).CreateLiveQRDisplay(ctx, store.CreateLiveQRDisplayParams{TenantID: tenantID, LocationID: venue, PairingCodeHash: code, TtlSeconds: 300})
		screen = row.ID
		return e
	}); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	const n = 32
	var (
		wg            sync.WaitGroup
		mu            sync.Mutex
		wins, refused int
		winner        string
		others        []error
		start         = make(chan struct{})
	)
	for range n {
		session := opRandHex(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, e := store.New(tx).ConsumeLiveQRDisplayPairing(ctx, store.ConsumeLiveQRDisplayPairingParams{
					SessionHash: session, TenantID: tenantID, ID: screen, PairingCodeHash: code,
				})
				return e
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
				winner = session
			case errors.Is(err, pgx.ErrNoRows):
				refused++
			default:
				others = append(others, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(others) > 0 {
		t.Fatalf("%d pairing(s) failed for another reason, first: %v", len(others), others[0])
	}
	if wins != 1 || refused != n-1 {
		t.Fatalf("%d goroutines consumed one code: %d succeeded, %d were refused; want exactly 1 and %d", n, wins, refused, n-1)
	}
	var stored int64
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM live_qr_displays WHERE tenant_id = $1 AND id = $2 AND pairing_code_hash IS NULL AND paired_at IS NOT NULL`,
			tenantID, screen).Scan(&stored)
	}); err != nil || stored != 1 {
		t.Fatalf("the screen after the race: %d paired rows (%v), want 1", stored, err)
	}
	var match bool
	if err := d.pool.QueryRow(context.Background(), `SELECT count(*) = 1 FROM resolve_live_qr_display_by_session_hash($1) WHERE id = $2`, winner, screen).Scan(&match); err != nil || !match {
		t.Fatalf("the winner's session does not resolve to the screen (%v)", err)
	}
	t.Logf("%d concurrent pairings of one code: %d succeeded, %d refused", n, wins, refused)
}

// TestLiveQRDisplays00111_DownRefusesWithRowsAndRoundTrips: the Down refuses while the table holds a
// row (55000) and changes nothing; with it empty (the rows set aside inside the transaction) it takes
// the table and the four functions away -- and tappa_resolver's three locations columns with them --
// and the Up builds the same objects and grants again. The tables the Down locks are taken first
// (lockForDowns: its DROP takes ACCESS EXCLUSIVE on locations and tenants, the two keys' tables).
func TestLiveQRDisplays00111_DownRefusesWithRowsAndRoundTrips(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00111File)
	lockForDowns(t, ctx, tx, []string{"live_qr_displays", "locations", "tenants"})
	objects := `SELECT (SELECT count(*) FROM pg_class WHERE relname = 'live_qr_displays' AND relnamespace = 'public'::regnamespace)
	                 + (SELECT count(*) FROM pg_proc WHERE proname IN ('resolve_live_qr_display_by_pairing_code_hash',
	                    'resolve_live_qr_display_by_session_hash', 'resolve_live_qr_display_by_id', 'live_qr_displays_state_is_one_way'))`
	if n := opInt(t, ctx, tx, objects); n != 5 {
		t.Fatalf("%d of 00111's five objects at HEAD", n)
	}
	w := newAppDeviceWorld(t, ctx, tx)
	w.addScreen(t, ctx, tx)
	opWant(t, holdingTryDown(t, ctx, tx, down), sqlstatePrerequisiteState, "00111's Down while a screen exists")
	if n := opInt(t, ctx, tx, objects); n != 5 {
		t.Fatalf("the refused Down changed something: %d objects", n)
	}
	if _, err := tx.Exec(ctx, liveQRSetAsideSQL); err != nil {
		t.Fatalf("set the rows aside inside the transaction: %v", err)
	}
	opRunSection(t, ctx, tx, down, "00111 Down")
	if n := opInt(t, ctx, tx, objects); n != 0 {
		t.Fatalf("%d of 00111's objects survive the Down", n)
	}
	if got := opColumns(t, ctx, tx, "tappa_resolver", "locations", "SELECT"); got != "" {
		t.Fatalf("after the Down tappa_resolver still reads %q of locations", got)
	}
	if extra := strongTableLocksOutside(t, ctx, tx, []string{"live_qr_displays", "locations", "tenants"}); len(extra) > 0 {
		t.Errorf("00111's Down took a strong lock on %v, outside the three it names", extra)
	}
	opRunSection(t, ctx, tx, up, "00111 Up again")
	if n := opInt(t, ctx, tx, objects); n != 5 {
		t.Fatalf("%d of 00111's objects after the Up, want 5", n)
	}
	if got := opColumns(t, ctx, tx, "tappa_resolver", "locations", "SELECT"); got != "id,tenant_id,active" {
		t.Fatalf("after the Up tappa_resolver reads %q of locations", got)
	}
	if got := opColumns(t, ctx, tx, "tappa_app", "live_qr_displays", "UPDATE"); got != "pairing_code_hash,paired_at,session_hash,revoked_at" {
		t.Fatalf("after the Up tappa_app updates %q", got)
	}
}

// TestLiveQRDisplays_ADeadCodeFreesItsValue is the M16-19 security review's ORTA: a code's digest lives
// only as long as the code can be used, so a dead screen does not hold its value in the global UNIQUE
// (with a short code, one tenant's dead screens would refuse another tenant's new code -- 23505):
//   - CONTROL: while A's screen waits, B cannot create a screen with the same digest (23505);
//   - a revocation drops the digest in the same statement, and B then can;
//   - an expired pairing keeps its digest (23505 again) until ClearExpiredLiveQRPairingCodes, run in
//     A's context, drops it -- and only it: a live waiting code and B's codes stay;
//   - a paired screen holds no code before or after its revocation;
//   - one way: a dropped code is not written back, a code-less screen does not pair (directly or
//     through the product's query), a live waiting code cannot be dropped by hand, a revoked screen's
//     leftover code (an owner's bare stamp) is dropped by RevokeLiveQRDisplay, and a screen is born
//     waiting -- an INSERT with no code is refused, for tappa_app and for the owner.
func TestLiveQRDisplays_ADeadCodeFreesItsValue(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	create := func(w appDeviceWorld, code string) error {
		return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
			_, err := store.New(sp).CreateLiveQRDisplay(ctx, store.CreateLiveQRDisplayParams{TenantID: w.tenant, LocationID: w.venue, PairingCodeHash: code, TtlSeconds: 600})
			return err
		})
	}
	revoke := func(w appDeviceWorld, id uuid.UUID) {
		if err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
			_, err := store.New(sp).RevokeLiveQRDisplay(ctx, store.RevokeLiveQRDisplayParams{TenantID: w.tenant, ID: id})
			return err
		}); err != nil {
			t.Fatalf("revoke: %v", err)
		}
	}
	codeOf := func(id uuid.UUID) *string {
		var c *string
		if err := tx.QueryRow(ctx, `SELECT pairing_code_hash FROM live_qr_displays WHERE id = $1`, id).Scan(&c); err != nil {
			t.Fatal(err)
		}
		return c
	}
	exec := func(w appDeviceWorld, sql string, args ...any) error {
		return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, sql, args...)
			return err
		})
	}

	// Revocation.
	waiting, code := a.addScreen(t, ctx, tx)
	opWant(t, create(b, code), sqlstateUniqueViolation, "CONTROL: B drawing the digest A's waiting screen holds")
	revoke(a, waiting)
	if c := codeOf(waiting); c != nil {
		t.Fatal("a revoked waiting screen still holds its code's digest")
	}
	if err := create(b, code); err != nil {
		t.Fatalf("B drawing a revoked screen's digest: %v, want it free", err)
	}

	// Expiry: the owner writes a past lifetime (tappa_app cannot set created_at).
	expired, expiredCode := uuid.New(), opRandHex(t)
	if _, err := tx.Exec(ctx, `INSERT INTO live_qr_displays (id, tenant_id, location_id, pairing_code_hash, created_at, pairing_expires_at)
	                           VALUES ($1, $2, $3, $4, now() - interval '2 hours', now() - interval '90 minutes')`, expired, a.tenant, a.venue, expiredCode); err != nil {
		t.Fatal(err)
	}
	live, liveCode := a.addScreen(t, ctx, tx)
	bLive, _ := b.addScreen(t, ctx, tx)
	opWant(t, create(b, expiredCode), sqlstateUniqueViolation, "CONTROL: B drawing an expired, unswept digest")
	opWant(t, exec(a, `UPDATE live_qr_displays SET pairing_code_hash = NULL WHERE id = $1`, live), sqlstateRestrictViolation,
		"dropping a live waiting code by hand")
	var swept int64
	if err := a.asApp(t, ctx, tx, a.tenant, func(sp pgx.Tx) error {
		var err error
		swept, err = store.New(sp).ClearExpiredLiveQRPairingCodes(ctx, a.tenant)
		return err
	}); err != nil || swept != 1 {
		t.Fatalf("the sweep dropped %d code(s) (%v), want the one expired", swept, err)
	}
	if codeOf(expired) != nil || codeOf(live) == nil || *codeOf(live) != liveCode || codeOf(bLive) == nil {
		t.Fatal("the sweep did not drop exactly the expired code: a live code, or another tenant's, was touched")
	}
	if err := create(b, expiredCode); err != nil {
		t.Fatalf("B drawing a swept digest: %v, want it free", err)
	}

	// A paired screen holds no code, before and after its revocation.
	paired, pairedCode := a.addScreen(t, ctx, tx)
	if _, err := a.pairScreen(t, ctx, tx, paired, pairedCode); err != nil {
		t.Fatal(err)
	}
	revoke(a, paired)
	if codeOf(paired) != nil {
		t.Fatal("a paired, revoked screen holds a code")
	}

	// One way.
	opWant(t, exec(a, `UPDATE live_qr_displays SET pairing_code_hash = $2 WHERE id = $1`, waiting, opRandHex(t)), sqlstateRestrictViolation,
		"a code written back onto a revoked screen")
	opWant(t, exec(a, `UPDATE live_qr_displays SET pairing_code_hash = $2 WHERE id = $1`, expired, opRandHex(t)), sqlstateRestrictViolation,
		"a code written back onto a swept screen")
	opWant(t, opTry(t, ctx, tx, `UPDATE live_qr_displays SET pairing_code_hash = $2 WHERE id = $1`, expired, opRandHex(t)), sqlstateRestrictViolation,
		"the owner writing a code back")
	opWant(t, exec(a, `UPDATE live_qr_displays SET paired_at = now(), session_hash = $2 WHERE id = $1`, expired, opRandHex(t)), sqlstateRestrictViolation,
		"pairing a screen whose code was dropped")
	if _, err := a.pairScreen(t, ctx, tx, expired, expiredCode); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the product's pairing of a swept code: %v, want pgx.ErrNoRows", err)
	}
	// A leftover code on a revoked screen (an owner's bare stamp) is dropped by the product's revoke.
	if _, err := tx.Exec(ctx, `UPDATE live_qr_displays SET revoked_at = now() WHERE id = $1`, live); err != nil {
		t.Fatal(err)
	}
	if codeOf(live) == nil {
		t.Fatal("PREMISE: the bare stamp dropped the code")
	}
	revoke(a, live)
	if codeOf(live) != nil {
		t.Fatal("the product's revoke left a revoked screen's code")
	}

	// Born waiting.
	opWant(t, exec(a, `INSERT INTO live_qr_displays (tenant_id, location_id, pairing_code_hash, pairing_expires_at)
	                   VALUES ($1, $2, NULL, now() + interval '5 minutes')`, a.tenant, a.venue), sqlstateRestrictViolation,
		"tappa_app inserting a screen with no code")
	opWant(t, opTry(t, ctx, tx, `INSERT INTO live_qr_displays (tenant_id, location_id, pairing_expires_at, revoked_at)
	                              VALUES ($1, $2, now() + interval '5 minutes', now())`, a.tenant, a.venue), sqlstateRestrictViolation,
		"the owner inserting a screen born dead")
}
