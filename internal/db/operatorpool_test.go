package db

// operatorpool_test.go -- M10 OP-7: the operator's pool (OperatorDB) and the startup
// parameter both pools pin (logparams.go, backlog T79 for the customer pool).
//
// HOW THE OPERATOR ROLE IS REACHED: tappa_operator is born NOLOGIN (01-roles.sql), so
// the configured-pool tests open the pool through openOperatorDB with a per-connection
// hook that turns the OWNER's connection into tappa_operator (SET SESSION
// AUTHORIZATION -- both identities the gate reads change). Everything after the hook
// -- the startup parameter, its read-back, the role gate -- is the production path.
// The refusals are driven through the exported NewOperatorDB, with no hook at all.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atknatk/tappa/internal/config"
)

// ----------------------------------------------------------------- helpers --

func appAndOwnerDSN(t *testing.T) (app, owner string) {
	t.Helper()
	app, owner = os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if app == "" || owner == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres required -- CLAUDE.md §8)")
	}
	return app, owner
}

// withParam appends one query parameter to a postgres URL.
func withParam(dsn, k, v string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + url.QueryEscape(k) + "=" + url.QueryEscape(v)
}

// asOperator is the impersonation hook: the owner's connection becomes tappa_operator
// for its whole life (session_user and current_user both).
func asOperator(ctx context.Context, c *pgx.Conn) error {
	_, err := c.Exec(ctx, `SET SESSION AUTHORIZATION tappa_operator`)
	return err
}

// unpinAfterStartup sets the pinned setting AFTER startup -- the shape of a connection
// the startup parameter did not reach (a pooler that drops it). A session SET outranks
// the startup parameter, so the read-back that follows sees -1.
func unpinAfterStartup(ctx context.Context, c *pgx.Conn) error {
	_, err := c.Exec(ctx, `SET log_parameter_max_length_on_error = -1`)
	return err
}

func chain(hooks ...func(context.Context, *pgx.Conn) error) func(context.Context, *pgx.Conn) error {
	return func(ctx context.Context, c *pgx.Conn) error {
		for _, h := range hooks {
			if err := h(ctx, c); err != nil {
				return err
			}
		}
		return nil
	}
}

// settingOn reads the pinned setting, session_user and current_user on one of the
// pool's connections.
func settingOn(t *testing.T, ctx context.Context, p *pgxpool.Pool) (setting, session, current string) {
	t.Helper()
	if err := p.QueryRow(ctx, `SELECT current_setting('log_parameter_max_length_on_error'), session_user, current_user`).
		Scan(&setting, &session, &current); err != nil {
		t.Fatalf("read the setting on the pool: %v", err)
	}
	return setting, session, current
}

// rawSetting connects WITHOUT the pin and reads the setting: the positive control that
// a role default or a DSN parameter is really live for a connection that does not pin.
func rawSetting(t *testing.T, ctx context.Context, dsn string) string {
	t.Helper()
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("raw connect for the control: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	var v string
	if err := c.QueryRow(ctx, readLogParameterSQL).Scan(&v); err != nil {
		t.Fatalf("raw read: %v", err)
	}
	return v
}

func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// --------------------------------------------- the pin (both pools, T79) --

// TestPin_TheStartupParameterOverridesARoleDefault is T79's measurement kept as a
// test: tappa_app is given the very role default the backlog item describes
// (log_parameter_max_length_on_error = -1), and the customer pool's connection still
// reads 0.
//
// 🔴 IT COMMITS A ROLE SETTING, AND THAT IS WHY IT IS SCOPED TO THE MAINTENANCE
// DATABASE. A role default applies at LOGIN, so it cannot be measured from inside a
// rolled-back transaction. `ALTER ROLE tappa_app IN DATABASE postgres SET ...` reaches
// only connections to the maintenance database -- none of this product's tests connect
// there except TestPing_DoesNotDependOnTheSchema, which goes through New and is pinned
// -- and Cleanup RESETs it and checks that the row is gone. If a run dies in between,
// TestPin_NoRoleLevelSettingOnTheConnectingRoles turns red on the next one and names
// the row.
//
// POSITIVE CONTROL: a raw connection with no startup parameter reads -1, so the role
// default is live and the 0 below is the pin's doing.
func TestPin_TheStartupParameterOverridesARoleDefault(t *testing.T) {
	app, owner := appAndOwnerDSN(t)
	maintenance, ok := swapDatabase(app, "postgres")
	if !ok {
		t.Skipf("DATABASE_URL (%d chars) is not in a form this test can repoint at the maintenance database", len(app))
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	oc, err := pgx.Connect(ctx, owner)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	defer func() { _ = oc.Close(context.Background()) }()
	if _, err := oc.Exec(ctx, `ALTER ROLE tappa_app IN DATABASE postgres SET log_parameter_max_length_on_error = -1`); err != nil {
		t.Skipf("this measurement needs a role default on the maintenance database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), owner)
		if err != nil {
			t.Errorf("reconnect to RESET the role default: %v -- run: ALTER ROLE tappa_app IN DATABASE postgres RESET log_parameter_max_length_on_error", err)
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		if _, err := c.Exec(context.Background(), `ALTER ROLE tappa_app IN DATABASE postgres RESET log_parameter_max_length_on_error`); err != nil {
			t.Errorf("RESET the role default: %v", err)
		}
		var n int
		if err := c.QueryRow(context.Background(), `SELECT count(*) FROM pg_db_role_setting WHERE setrole = 'tappa_app'::regrole`).Scan(&n); err != nil || n != 0 {
			t.Errorf("after RESET, pg_db_role_setting holds %d row(s) for tappa_app (err %v)", n, err)
		}
	})

	if got := rawSetting(t, ctx, maintenance); got != "-1" {
		t.Fatalf("CONTROL FAILED: a connection with no startup parameter reads %q, not the role default -1; "+
			"this test cannot show the pin overriding anything", got)
	}

	d, err := New(ctx, &config.Config{DatabaseURL: maintenance})
	if err != nil {
		t.Fatalf("New refused a pool whose startup parameter outranks the role default: %v", err)
	}
	defer d.Close()
	if got, _, _ := settingOn(t, ctx, d.pool); got != "0" {
		t.Fatalf("the customer pool's connection reads log_parameter_max_length_on_error=%q under a role default of -1; "+
			"the startup parameter did not win, and every failing statement's bound parameters reach the log", got)
	}
}

// TestPin_ADSNCannotUnpinIt: pgx turns an unknown DSN query parameter into a startup
// parameter, so a DSN can carry the setting itself. The pin overwrites it, on both
// pools. Control: the same DSN through a raw connection really does arrive at -1.
func TestPin_ADSNCannotUnpinIt(t *testing.T) {
	app, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	t.Run("customer pool", func(t *testing.T) {
		dsn := withParam(app, logParameterPin, "-1")
		if got := rawSetting(t, ctx, dsn); got != "-1" {
			t.Fatalf("CONTROL FAILED: the DSN parameter does not reach a raw connection (%q)", got)
		}
		d, err := New(ctx, &config.Config{DatabaseURL: dsn})
		if err != nil {
			t.Fatalf("New refused a DSN the pin should simply overwrite: %v", err)
		}
		defer d.Close()
		if got, _, _ := settingOn(t, ctx, d.pool); got != "0" {
			t.Fatalf("a DSN carrying %s=-1 unpinned the customer pool (%q)", logParameterPin, got)
		}
	})
	t.Run("operator pool", func(t *testing.T) {
		dsn := withParam(owner, logParameterPin, "-1")
		if got := rawSetting(t, ctx, dsn); got != "-1" {
			t.Fatalf("CONTROL FAILED: the DSN parameter does not reach a raw connection (%q)", got)
		}
		o, err := openOperatorDB(ctx, dsn, asOperator)
		if err != nil {
			t.Fatalf("the operator pool refused a DSN the pin should simply overwrite: %v", err)
		}
		defer o.Close()
		if got, _, _ := settingOn(t, ctx, o.pool); got != "0" {
			t.Fatalf("a DSN carrying %s=-1 unpinned the operator pool (%q)", logParameterPin, got)
		}
	})
}

// TestPin_AConnectionTheParameterDidNotReachIsRefused is the read-back's own test: a
// connection on which the setting is not 0 at the end of startup is refused -- at open
// (both pools: no pool is handed back) and on every LATER connection (the claim
// logparams.go makes: the check is per connection, not once at boot).
func TestPin_AConnectionTheParameterDidNotReachIsRefused(t *testing.T) {
	app, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	t.Run("customer pool at open", func(t *testing.T) {
		d, err := newDB(ctx, &config.Config{DatabaseURL: app}, unpinAfterStartup)
		var np *logParameterNotPinnedError
		if err == nil || !errors.As(err, &np) || np.Got != "-1" {
			if d != nil {
				d.Close()
			}
			t.Fatalf("an unpinned connection opened the customer pool (err %v)", err)
		}
		if d != nil {
			t.Fatal("the customer pool refused AND returned a pool")
		}
		// CONTROL: the same path without the late SET opens.
		ok, err := newDB(ctx, &config.Config{DatabaseURL: app}, nil)
		if err != nil {
			t.Fatalf("CONTROL FAILED: the customer pool does not open at all: %v", err)
		}
		ok.Close()
	})
	t.Run("operator pool at open", func(t *testing.T) {
		o, err := openOperatorDB(ctx, owner, chain(asOperator, unpinAfterStartup))
		var np *logParameterNotPinnedError
		if err == nil || !errors.As(err, &np) || np.Got != "-1" {
			if o != nil {
				o.Close()
			}
			t.Fatalf("an unpinned connection opened the operator pool (err %v)", err)
		}
		if errors.Is(err, ErrOperatorUnreachable) {
			t.Fatal("the read-back's refusal is marked unreachable; cmd/tappa would keep booting on an unpinned connection")
		}
		ok, err := openOperatorDB(ctx, owner, asOperator)
		if err != nil {
			t.Fatalf("CONTROL FAILED: the operator pool does not open through the hook: %v", err)
		}
		ok.Close()
	})
	t.Run("a later connection", func(t *testing.T) {
		// The first connection is clean; every later one is not. The pool opens (its
		// ping used the first), and the SECOND physical connection is refused.
		var n atomic.Int32
		laterUnpinned := func(ctx context.Context, c *pgx.Conn) error {
			if n.Add(1) > 1 {
				return unpinAfterStartup(ctx, c)
			}
			return nil
		}
		d, err := newDB(ctx, &config.Config{DatabaseURL: app}, laterUnpinned)
		if err != nil {
			t.Fatalf("the first connection was clean, yet the pool did not open: %v", err)
		}
		defer d.Close()
		first, err := d.pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire the first connection: %v", err)
		}
		defer first.Release()
		second, err := d.pool.Acquire(ctx)
		var np *logParameterNotPinnedError
		if err == nil || !errors.As(err, &np) {
			if second != nil {
				second.Release()
			}
			t.Fatalf("a connection opened AFTER start-up with the setting at -1 was handed out (err %v); the "+
				"read-back runs only at boot", err)
		}
	})
	// 2i (the 6th closing auditor): the same for the OPERATOR pool, through its test hook
	// (asOperator on every connection, the late SET from the second one on).
	t.Run("a later connection, operator pool", func(t *testing.T) {
		var n atomic.Int32
		laterUnpinned := func(ctx context.Context, c *pgx.Conn) error {
			if err := asOperator(ctx, c); err != nil {
				return err
			}
			if n.Add(1) > 1 {
				return unpinAfterStartup(ctx, c)
			}
			return nil
		}
		o, err := openOperatorDB(ctx, owner, laterUnpinned)
		if err != nil {
			t.Fatalf("the first connection was clean, yet the operator pool did not open: %v", err)
		}
		defer o.Close()
		first, err := o.pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire the first connection: %v", err)
		}
		defer first.Release()
		second, err := o.pool.Acquire(ctx)
		var np *logParameterNotPinnedError
		if err == nil || !errors.As(err, &np) {
			if second != nil {
				second.Release()
			}
			t.Fatalf("a connection the operator pool opened AFTER start-up with the setting at -1 was handed out "+
				"(err %v)", err)
		}
	})
}

// TestPin_NoRoleLevelSettingOnTheConnectingRoles is T79's catalogue pin, the twin of
// OP-5's operator-role check in TestOperator00026_ReverseCatalogPin: no
// pg_db_role_setting row for the two roles this process connects as, nor for the
// operator definer -- in any database. The startup parameter already outranks such a
// row; this is the cheap second net that SAYS so when one appears (a leaked DSN used
// once leaves exactly this trace).
//
// CONTROLS (inside a rolled-back transaction): the same finder sees a role-wide row,
// a per-database row, and a row on the operator role.
func TestPin_NoRoleLevelSettingOnTheConnectingRoles(t *testing.T) {
	_, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	conn, err := pgx.Connect(ctx, owner)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	find := func(q pgx.Tx) []string {
		t.Helper()
		rows, err := q.Query(ctx, `
			SELECT pg_get_userbyid(s.setrole) || ' in ' || coalesce((SELECT datname FROM pg_database WHERE oid = s.setdatabase), 'every database')
			       || ': ' || array_to_string(s.setconfig, ',')
			  FROM pg_db_role_setting s
			 WHERE s.setrole IN ('tappa_app'::regrole, 'tappa_operator'::regrole, 'tappa_opdefiner'::regrole)`)
		if err != nil {
			t.Fatalf("read pg_db_role_setting: %v", err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}
	if found := find(tx); len(found) != 0 {
		t.Errorf("role-level settings on the connecting roles: %v. Remove each with ALTER ROLE <role> [IN DATABASE <db>] "+
			"RESET <setting> (backlog T79; the startup parameter outranks it, but a row here is the trace of a DSN "+
			"used to write it)", found)
	}
	for _, ddl := range []string{
		`ALTER ROLE tappa_app SET log_parameter_max_length_on_error = -1`,
		`ALTER ROLE tappa_app IN DATABASE postgres SET log_parameter_max_length_on_error = -1`,
		`ALTER ROLE tappa_operator SET log_parameter_max_length_on_error = -1`,
	} {
		t.Run("control/"+ddl, func(t *testing.T) {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatalf("savepoint: %v", err)
			}
			defer func() { _ = sp.Rollback(ctx) }()
			if _, err := sp.Exec(ctx, ddl); err != nil {
				t.Fatalf("apply the control: %v", err)
			}
			if found := find(sp); len(found) == 0 {
				t.Fatalf("the finder does not see %q", ddl)
			}
		})
	}
}

// ---------------------------------------------- the operator pool's role gate --

// TestOperatorRoleRefusal_IsTappaOperatorAndNothingMore is the gate's truth table: one
// row that passes and one row per way to fail, so a predicate that dropped any single
// condition stays red on that row.
func TestOperatorRoleRefusal_IsTappaOperatorAndNothingMore(t *testing.T) {
	ok := operatorRoleFacts{RoleFacts: RoleFacts{User: "tappa_operator", Session: "tappa_operator"}}
	if err := operatorRoleRefusal(ok); err != nil {
		t.Fatalf("tappa_operator with no privilege and no membership is refused: %v", err)
	}
	mut := func(f func(*operatorRoleFacts)) operatorRoleFacts { c := ok; f(&c); return c }
	for name, facts := range map[string]operatorRoleFacts{
		"another session user (the owner wearing role=)": mut(func(f *operatorRoleFacts) { f.Session = "tappa_owner" }),
		"another current user":                           mut(func(f *operatorRoleFacts) { f.User = "tappa_app" }),
		"both another role":                              mut(func(f *operatorRoleFacts) { f.Session, f.User = "tappa_app", "tappa_app" }),
		"empty names":                                    {},
		// The session NAME check is the first layer; RoleFacts.Privileged's switched-
		// session rule is the second, and it reads "" as "not read". This row is the
		// first layer on its own: a reader that forgot session_user is refused.
		"session not read":              mut(func(f *operatorRoleFacts) { f.Session = "" }),
		"superuser":                     mut(func(f *operatorRoleFacts) { f.Super = true }),
		"bypassrls":                     mut(func(f *operatorRoleFacts) { f.BypassRLS = true }),
		"owner of a row-security table": mut(func(f *operatorRoleFacts) { f.OwnsScopedTable = true }),
		"member of a privileged role":   mut(func(f *operatorRoleFacts) { f.InheritsPrivilege = true }),
		"member of an ordinary role":    mut(func(f *operatorRoleFacts) { f.MemberOfAny = true }),
		"has a member":                  mut(func(f *operatorRoleFacts) { f.HasMembers = true }),
		"CREATEDB":                      mut(func(f *operatorRoleFacts) { f.CreateDB = true }),
		"CREATEROLE":                    mut(func(f *operatorRoleFacts) { f.CreateRole = true }),
		"REPLICATION":                   mut(func(f *operatorRoleFacts) { f.Replication = true }),
	} {
		err := operatorRoleRefusal(facts)
		if err == nil {
			t.Errorf("%s: not refused (%+v)", name, facts)
			continue
		}
		if !strings.Contains(err.Error(), "tappa_operator") {
			t.Errorf("%s: the refusal does not say which role is required: %v", name, err)
		}
	}
}

// TestOperatorDB_RefusesEveryRoleButTappaOperator drives the EXPORTED constructor
// against the real server with the DSNs a deployment could plausibly paste into
// TAPPA_OPERATOR_DATABASE_URL: the owner's (a superuser), the customer's (tappa_app),
// and the owner's with `role=tappa_operator` -- the forged current_user. Each is refused
// in development and in production alike, and no pool is handed back. The positive
// half is the hook path: a session that IS tappa_operator opens, pinned.
func TestOperatorDB_RefusesEveryRoleButTappaOperator(t *testing.T) {
	app, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	// CONTROL for the forged case: the startup parameter really does make the owner's
	// session wear tappa_operator as its current_user, so the refusal below is the
	// session_user half of the gate at work and not a failed connection.
	forged := withParam(owner, "role", "tappa_operator")
	func() {
		c, err := pgx.Connect(ctx, forged)
		if err != nil {
			t.Fatalf("CONTROL FAILED: the owner cannot connect with role=tappa_operator: %v", err)
		}
		defer func() { _ = c.Close(context.Background()) }()
		var su, cu string
		if err := c.QueryRow(ctx, `SELECT session_user, current_user`).Scan(&su, &cu); err != nil || cu != "tappa_operator" || su == "tappa_operator" {
			t.Fatalf("CONTROL FAILED: role=tappa_operator gave session_user=%q current_user=%q (err %v)", su, cu, err)
		}
	}()

	for _, env := range []string{config.EnvDev, config.EnvProd} {
		for _, tc := range []struct{ name, dsn, want string }{
			{"the owner (a superuser)", owner, `session_user="tappa_owner"`},
			{"the customer role", app, `session_user="tappa_app"`},
			{"the owner wearing role=tappa_operator", forged, `current_user="tappa_operator"`},
		} {
			t.Run(env+"/"+tc.name, func(t *testing.T) {
				o, err := NewOperatorDB(ctx, &config.Config{Env: env, OperatorDatabaseURL: tc.dsn})
				if err == nil {
					o.Close()
					t.Fatalf("NewOperatorDB opened a pool for %s with TAPPA_ENV=%s", tc.name, env)
				}
				if o != nil {
					t.Fatal("NewOperatorDB refused AND returned a pool")
				}
				if !strings.Contains(err.Error(), "refusing to open") || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("the refusal is not the role gate's, or does not report what it measured: %v", err)
				}
				if errors.Is(err, ErrOperatorUnreachable) {
					t.Errorf("the role gate's refusal is marked unreachable; cmd/tappa would keep booting on a wrong role")
				}
			})
		}
	}

	t.Run("tappa_operator opens, pinned", func(t *testing.T) {
		o, err := openOperatorDB(ctx, owner, asOperator)
		if err != nil {
			t.Fatalf("a session that IS tappa_operator was refused: %v", err)
		}
		defer o.Close()
		setting, session, current := settingOn(t, ctx, o.pool)
		if setting != "0" || session != "tappa_operator" || current != "tappa_operator" {
			t.Fatalf("the operator pool's connection reads setting=%q session_user=%q current_user=%q", setting, session, current)
		}
	})

	t.Run("no DSN, a malformed DSN", func(t *testing.T) {
		for _, dsn := range []string{"", "postgres://tappa_operator@127.0.0.1:notaport/tappa"} {
			o, err := NewOperatorDB(ctx, &config.Config{OperatorDatabaseURL: dsn})
			if err == nil || o != nil {
				t.Fatalf("a %d-byte DSN opened a pool", len(dsn))
			}
			if errors.Is(err, ErrOperatorUnreachable) {
				t.Errorf("a %d-byte DSN is a configuration error, not unreachability: %v", len(dsn), err)
			}
		}
	})
}

// TestOperatorRoleQuery_SeesAMembershipOfAnyKind runs the SHIPPED operatorRoleQuery as
// tappa_operator inside a rolled-back transaction, before and after making it a member
// of tappa_app -- an ORDINARY role, which roleFactsQuery's InheritsPrivilege does not
// see. That gap is why the operator gate reads membership of any kind.
func TestOperatorRoleQuery_SeesAMembershipOfAnyKind(t *testing.T) {
	ctx, tx := opTx(t)
	read := func() (facts operatorRoleFacts) {
		t.Helper()
		if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) (err error) {
			facts, err = readOperatorRoleOn(ctx, sp)
			return err
		}); err != nil {
			t.Fatalf("run the role queries as tappa_operator: %v", err)
		}
		return facts
	}
	before := read()
	if before.MemberOfAny || before.Session != "tappa_operator" || operatorRoleRefusal(before) != nil {
		t.Fatalf("tappa_operator as shipped reads %+v; the gate must pass it", before)
	}
	if _, err := tx.Exec(ctx, `GRANT tappa_app TO tappa_operator`); err != nil {
		t.Fatalf("grant: %v", err)
	}
	after := read()
	if !after.MemberOfAny {
		t.Fatalf("tappa_operator as a member of tappa_app reads member_of_any_role=false (%+v)", after)
	}
	if after.InheritsPrivilege {
		t.Fatalf("PREMISE: tappa_app is not privileged, so InheritsPrivilege should stay false (%+v)", after)
	}
	if operatorRoleRefusal(after) == nil {
		t.Fatal("the gate opens a pool for tappa_operator as a member of tappa_app")
	}
}

// ---------------------------------------------------- the operator pool's shape --

// TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor pins, by reflection, what OperatorDB
// does NOT offer (ADR 0021 §3.6 and the OP-7 card): no WithTenant; not itself an
// OperatorConn (no Exec/QueryRow: the SQL is operator.go's); no method that takes a
// callback (WithTenant's shape: a function handed a transaction) and no method whose
// signature carries a pgx or pgxpool type (a handle to the connection).
func TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor(t *testing.T) {
	rt := reflect.TypeOf((*OperatorDB)(nil))
	if _, ok := rt.MethodByName("WithTenant"); ok {
		t.Error("OperatorDB has a WithTenant method; the operator crosses the tenant boundary through op_* only (ADR 0021 §3.6)")
	}
	if rt.Implements(reflect.TypeOf((*OperatorConn)(nil)).Elem()) {
		t.Error("*OperatorDB is an OperatorConn: any package holding it could run SQL text of its own on the operator's connection")
	}
	var mentions func(reflect.Type, int) bool
	mentions = func(t reflect.Type, depth int) bool {
		if depth > 4 {
			return false
		}
		if p := t.PkgPath(); strings.HasPrefix(p, "github.com/jackc/pgx") {
			return true
		}
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
			return mentions(t.Elem(), depth+1)
		case reflect.Map:
			return mentions(t.Key(), depth+1) || mentions(t.Elem(), depth+1)
		}
		return false
	}
	if rt.NumMethod() < 14 {
		t.Fatalf("PREMISE: *OperatorDB has %d methods; Close, the seven Store methods, the two LegalStore methods (OP-10), the two TenantStore methods (OP-11), the PlaqueStore method (OP-13) and the AuditStore method (OP-14) should all be there", rt.NumMethod())
	}
	for i := 0; i < rt.NumMethod(); i++ {
		m := rt.Method(i)
		for j := 1; j < m.Type.NumIn(); j++ { // 0 is the receiver
			in := m.Type.In(j)
			if in.Kind() == reflect.Func {
				t.Errorf("%s takes a function (%s): the WithTenant shape -- a callback handed a connection", m.Name, in)
			}
			if mentions(in, 0) {
				t.Errorf("%s takes a pgx type (%s)", m.Name, in)
			}
		}
		for j := 0; j < m.Type.NumOut(); j++ {
			if mentions(m.Type.Out(j), 0) {
				t.Errorf("%s returns a pgx type (%s): a handle to the operator's connection", m.Name, m.Type.Out(j))
			}
		}
	}
}

// TestOperatorDB_EveryMethodDelegatesVerbatim reads operatorpool.go's syntax tree: every
// OperatorDB method but Close is a single `return F(ctx, o.pool, <its other parameters
// in order>)` where F has the method's own name and is operator.go's function -- so
// the SQL is written once, and an argument swapped between two same-typed parameters
// (three strings in CompleteOperatorEnrollment and in PublishLegal) does not compile into
// a green build. Thirteen methods: the seven operatorauth.Store ones, since OP-10 phase B
// LegalVersions and PublishLegal, since OP-11 phase B TenantList and TenantDetail, since
// OP-13 phase B TenantPlaques, and since OP-14 phase B OperatorAudit.
func TestOperatorDB_EveryMethodDelegatesVerbatim(t *testing.T) {
	fset := token.NewFileSet()
	pool, err := parser.ParseFile(fset, "operatorpool.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ops, err := parser.ParseFile(fset, "operator.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	freeFuncs := map[string]*ast.FuncDecl{}
	for _, d := range ops.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
			freeFuncs[fd.Name.Name] = fd
		}
	}
	paramNames := func(fl *ast.FieldList) (out []string) {
		for _, f := range fl.List {
			for _, n := range f.Names {
				out = append(out, n.Name)
			}
		}
		return out
	}
	delegating := 0
	for _, d := range pool.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || fd.Name.Name == "Close" {
			continue
		}
		name := fd.Name.Name
		recv := fd.Recv.List[0].Names[0].Name
		if len(fd.Body.List) != 1 {
			t.Errorf("%s: %d statements, want one return", name, len(fd.Body.List))
			continue
		}
		ret, ok := fd.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			t.Errorf("%s: not a single `return F(...)`", name)
			continue
		}
		call, ok := ret.Results[0].(*ast.CallExpr)
		if !ok {
			t.Errorf("%s: does not return a call", name)
			continue
		}
		if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != name || freeFuncs[name] == nil {
			t.Errorf("%s: does not call operator.go's function of the same name", name)
			continue
		}
		params := paramNames(fd.Type.Params)
		var got []string
		for _, a := range call.Args {
			switch e := a.(type) {
			case *ast.Ident:
				got = append(got, e.Name)
			case *ast.SelectorExpr:
				if x, ok := e.X.(*ast.Ident); ok {
					got = append(got, x.Name+"."+e.Sel.Name)
				}
			default:
				got = append(got, "?")
			}
		}
		want := append([]string{params[0], recv + ".pool"}, params[1:]...)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s passes (%s), want (%s): the arguments are not the method's own, in order", name,
				strings.Join(got, ", "), strings.Join(want, ", "))
		}
		// And operator.go's function takes the connection second, as an OperatorConn.
		fp := freeFuncs[name].Type.Params.List
		if len(fp) < 2 {
			t.Errorf("%s: operator.go's function has too few parameters", name)
		} else if id, ok := fp[1].Type.(*ast.Ident); !ok || id.Name != "OperatorConn" {
			t.Errorf("%s: operator.go's function does not take an OperatorConn second", name)
		}
		delegating++
	}
	if delegating != 13 {
		t.Fatalf("%d delegating methods on OperatorDB, want the seven operatorauth.Store methods, the two operator.LegalStore methods (OP-10), the two operator.TenantStore methods (OP-11), the operator.PlaqueStore method (OP-13) and the operator.AuditStore method (OP-14)", delegating)
	}
}

// TestOperatorDB_SetsNoTenantContext: no string literal in the operator pool's two files
// names set_config or app.tenant_id (ADR 0021 §3.6: no tenant context is produced or
// consumed). Control: the customer pool's tenant.go does name it, so the scan can see one.
func TestOperatorDB_SetsNoTenantContext(t *testing.T) {
	literals := func(file string) (out []string) {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if bl, ok := n.(*ast.BasicLit); ok && bl.Kind == token.STRING {
				if s, err := strconv.Unquote(bl.Value); err == nil {
					out = append(out, s)
				}
			}
			return true
		})
		return out
	}
	names := func(lits []string) bool {
		for _, s := range lits {
			if strings.Contains(s, "set_config") || strings.Contains(s, "app.tenant_id") {
				return true
			}
		}
		return false
	}
	for _, f := range []string{"operatorpool.go", "operator.go", "logparams.go"} {
		if names(literals(f)) {
			t.Errorf("%s sets or reads the tenant context", f)
		}
	}
	if !names(literals("tenant.go")) {
		t.Fatal("CONTROL FAILED: tenant.go names no tenant context; the scan cannot see one")
	}
}

// TestOperatorDB_RunsAsTappaOperator: the configured pool executes an op_* (it holds
// EXECUTE as tappa_operator) and reads platform_admins, through the methods -- the two
// refusals that write nothing, so the shared database keeps no row. The operator-tables
// test lock is taken SHARED (OP-6 md. 16) because both statements touch those tables.
func TestOperatorDB_RunsAsTappaOperator(t *testing.T) {
	_, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	lock, err := pgx.Connect(ctx, owner)
	if err != nil {
		t.Fatalf("connect for the lock: %v", err)
	}
	defer func() { _ = lock.Close(context.Background()) }()
	if _, err := lock.Exec(ctx, `SELECT pg_advisory_lock_shared(hashtext($1))`, operatorTablesTestLock); err != nil {
		t.Fatalf("operator-tables test lock: %v", err)
	}

	o, err := openOperatorDB(ctx, owner, asOperator)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer o.Close()
	if _, err := o.TouchOperatorSession(ctx, randHex(t, 32)); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("an unknown session hash: %v, want ErrOperatorRefused (28000 from op_touch_session)", err)
	}
	if _, err := o.OperatorByEmail(ctx, "nobody-"+randHex(t, 6)+"@example.test"); !errors.Is(err, ErrNoOperator) {
		t.Errorf("an unknown address: %v, want ErrNoOperator", err)
	}
}

// TestOperatorDB_UnreachabilityIsMarkedAndNothingElseIs: a failure to CONNECT -- a
// closed port, a wrong password (28P01), a database that does not exist (3D000), a
// context that is already over -- wraps ErrOperatorUnreachable (cmd/tappa keeps the
// customer product up on it); the context sentinel survives the wrapping. The refusals
// of what was reached are pinned NOT to carry it in the tests above.
func TestOperatorDB_UnreachabilityIsMarkedAndNothingElseIs(t *testing.T) {
	_, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	u, err := url.Parse(owner)
	if err != nil || u.User == nil {
		t.Skipf("DATABASE_MIGRATE_URL (%d chars) is not a URL with a user this test can rewrite", len(owner))
	}
	wrongPassword := *u
	wrongPassword.User = url.UserPassword(u.User.Username(), randHex(t, 16))
	missingDB := *u
	missingDB.Path = "/tappa_missing_" + randHex(t, 4)

	for _, tc := range []struct{ name, dsn, want string }{
		{"a closed port", sentinelDSN(randHex(t, 8)), "connection refused"},
		{"a wrong password", wrongPassword.String(), "SQLSTATE 28P01"},
		{"a database that does not exist", missingDB.String(), "SQLSTATE 3D000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := NewOperatorDB(ctx, &config.Config{OperatorDatabaseURL: tc.dsn})
			if err == nil {
				o.Close()
				t.Fatal("opened")
			}
			if !errors.Is(err, ErrOperatorUnreachable) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s: %v, want ErrOperatorUnreachable naming %q", tc.name, err, tc.want)
			}
		})
	}
	t.Run("a context that is already over", func(t *testing.T) {
		done, stop := context.WithCancel(ctx)
		stop()
		o, err := NewOperatorDB(done, &config.Config{OperatorDatabaseURL: owner})
		if err == nil {
			o.Close()
			t.Fatal("opened on a cancelled context")
		}
		if !errors.Is(err, ErrOperatorUnreachable) || !errors.Is(err, context.Canceled) {
			t.Errorf("a cancelled context: %v, want ErrOperatorUnreachable and context.Canceled", err)
		}
	})
}

// -------------------------------------------------------------- the leaks --

// leakVerbs are fmt's verbs, %p and %w included (OP-6's matrix: the paths that reach no
// method -- %p, %w -- print by reflection).
var leakVerbs = []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%X", "%d", "%o", "%O", "%b", "%e", "%E",
	"%f", "%F", "%g", "%G", "%t", "%c", "%U", "%p", "%w"}

type plainHolder struct{ dsn string }

// renderEverywhere prints v through every verb in five positions (itself, in an
// exported and an unexported field, behind an interface field, in a slice and a map),
// with Sprintf and Errorf, through slog's text and JSON handlers at DEBUG, and through
// encoding/json.
func renderEverywhere(v any) []string {
	type exported struct{ V any }
	type unexported struct{ v any }
	forms := []any{v, exported{v}, unexported{v}, &unexported{v}, []any{v}, map[string]any{"k": v}}
	var out []string
	for _, f := range forms {
		for _, verb := range leakVerbs {
			out = append(out, fmt.Sprintf(verb, f), fmt.Errorf(verb, f).Error())
		}
		for _, h := range []func(*strings.Builder) slog.Handler{
			func(b *strings.Builder) slog.Handler {
				return slog.NewTextHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug})
			},
			func(b *strings.Builder) slog.Handler {
				return slog.NewJSONHandler(b, &slog.HandlerOptions{Level: slog.LevelDebug})
			},
		} {
			var b strings.Builder
			slog.New(h(&b)).Debug("probe", "v", f)
			out = append(out, b.String())
		}
		if j, err := json.Marshal(f); err == nil {
			out = append(out, string(j))
		}
	}
	return out
}

func sentinelDSN(password string) string {
	u := url.URL{Scheme: "postgres", User: url.UserPassword("tappa_operator", password), Host: "127.0.0.1:1", Path: "/tappa"}
	return u.String() + "?sslmode=disable"
}

// TestOperatorDB_PrintsNoConnectionString renders an OperatorDB built around a pool
// whose configuration holds a sentinel password (pgxpool connects lazily: nothing is
// dialled) and finds neither the DSN nor the password anywhere in the matrix.
//
// POSITIVE CONTROLS: the same matrix finds the password in a struct holding the DSN as
// a PLAIN field -- the shape the OP-7 rule forbids -- so the matrix can see the class.
func TestOperatorDB_PrintsNoConnectionString(t *testing.T) {
	password := randHex(t, 16)
	dsn := sentinelDSN(password)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	o := &OperatorDB{pool: p}
	for _, v := range []any{o, *o} {
		for i, text := range renderEverywhere(v) {
			if strings.Contains(text, password) || strings.Contains(text, dsn) {
				t.Fatalf("rendering #%d of an OperatorDB carries the connection string's password", i)
			}
		}
	}
	found := false
	for _, text := range renderEverywhere(plainHolder{dsn: dsn}) {
		found = found || strings.Contains(text, password)
	}
	if !found {
		t.Fatal("CONTROL FAILED: the matrix does not find a password held in a plain field")
	}

	// AND THE VALUE THE CONSTRUCTOR BUILDS, not only the one this test assembled: an
	// OperatorDB opened through the production path (the hook only switches the role)
	// over the owner's real DSN. Searched for the DSN itself and its user:password@ part
	// -- the bare development password is too short to search for without matching the
	// role names an OperatorDB legitimately does not print anyway.
	_, owner := appAndOwnerDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	opened, err := openOperatorDB(ctx, owner, asOperator)
	if err != nil {
		t.Fatalf("open through the hook: %v", err)
	}
	defer opened.Close()
	needles := []string{owner}
	if u, err := url.Parse(owner); err == nil && u.User != nil {
		if pw, ok := u.User.Password(); ok {
			needles = append(needles, u.User.Username()+":"+pw+"@")
		}
	}
	for _, v := range []any{opened, *opened} {
		for i, text := range renderEverywhere(v) {
			for _, n := range needles {
				if strings.Contains(text, n) {
					t.Fatalf("rendering #%d of a constructed OperatorDB carries the connection string", i)
				}
			}
		}
	}
}

// TestOperatorDB_RefusalsCarryNoConnectionString drives the refusals that happen
// BEFORE a role is read -- an unparseable DSN, an unreachable server -- with sentinels
// in the DSN (the password, in the user-info part, as a query parameter and in
// keyword/value form; the user and database names) and searches each returned error's
// renderings for them. The role gate's refusal is not here: it prints the role names it
// measured, by design (TestOperatorDB_RefusesEveryRoleButTappaOperator), and runs only
// after a successful login.
//
// 🔴 THE QUERY-PARAMETER CASE IS A MEASURED HOLE IN pgx, NOT A HYPOTHETICAL: pgconn's
// ParseConfigError quotes the connection string through redactPW, which redacts only
// the user-info password of a URL. The control below shows pgxpool.ParseConfig's own
// error CARRYING the sentinel; the constructor's error must not.
func TestOperatorDB_RefusalsCarryNoConnectionString(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	// Sentinels for EVERY part of the DSN a message could quote: the password, and the
	// user and database names -- pgx's own connect error quotes the latter two
	// ("failed to connect to `user=... database=...`"), so finding them would mean pgx's
	// text reached the refusal, which the constructor promises it does not.
	password, user, dbname := randHex(t, 16), "opuser"+randHex(t, 6), "opdb"+randHex(t, 6)
	urlForm := func(host string) string {
		u := url.URL{Scheme: "postgres", User: url.UserPassword(user, password), Host: host, Path: "/" + dbname}
		return u.String() + "?sslmode=disable"
	}
	queryForm := "postgres://" + user + "@127.0.0.1:1/" + dbname + "?password=" + password + "&sslmode=bogus"

	if _, err := pgxpool.ParseConfig(queryForm); err == nil || !strings.Contains(err.Error(), password) {
		t.Fatalf("CONTROL: pgx's parse error no longer quotes a query-parameter password (err %v); the premise of "+
			"not wrapping it has changed -- re-measure before relying on pgx's text", err != nil)
	}
	// CONTROL: pgx's connect error does quote the user and the database, so the search
	// below can see pgx's text if it leaks.
	if c, err := pgx.Connect(ctx, urlForm("127.0.0.1:1")); err == nil {
		_ = c.Close(ctx)
		t.Fatal("CONTROL FAILED: something listens on 127.0.0.1:1")
	} else if !strings.Contains(err.Error(), user) || !strings.Contains(err.Error(), dbname) {
		t.Fatal("CONTROL FAILED: pgx's connect error no longer names the user and database; the search is blind to its text")
	}

	for _, tc := range []struct{ name, dsn string }{
		{"unreachable, URL user-info", urlForm("127.0.0.1:1")},
		{"unparseable, query parameter", queryForm},
		{"unparseable, bad port", urlForm("127.0.0.1:notaport")},
		{"unreachable, keyword/value", "host=127.0.0.1 port=1 user=" + user + " dbname=" + dbname + " password=" + password + " sslmode=disable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := NewOperatorDB(ctx, &config.Config{OperatorDatabaseURL: tc.dsn})
			if err == nil {
				o.Close()
				t.Fatal("the constructor opened a pool on a DSN that cannot work")
			}
			for i, text := range renderEverywhere(err) {
				for label, s := range map[string]string{"password": password, "user name": user, "database name": dbname} {
					if strings.Contains(text, s) {
						t.Fatalf("rendering #%d of the refusal carries the DSN's %s", i, label)
					}
				}
			}
		})
	}
}
